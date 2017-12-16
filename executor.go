package cmds

import (
	"context"
	"reflect"
	"time"

	"github.com/ipfs/go-ipfs-cmdkit"
)

type Executor interface {
	Execute(req *Request, re ResponseEmitter, env interface{}) error
}

func NewExecutor(root *Command) Executor {
	return &executor{
		//env:  env,
		root: root,
	}
}

type executor struct {
	//env  interface{}
	root *Command
}

func (x *executor) Execute(req *Request, re ResponseEmitter, env interface{}) (err error) {
	cmd := req.Command

	if cmd.Run == nil {
		return ErrNotCallable
	}

	err = cmd.CheckArguments(req)
	if err != nil {
		return err
	}

	// If this ResponseEmitter encodes messages (e.g. http, cli or writer - but not chan),
	// we need to update the encoding to the one specified by the command.
	if ee, ok := re.(EncodingEmitter); ok {
		encType := GetEncoding(req)

		// use JSON if text was requested but the command doesn't have a text-encoder
		if _, ok := cmd.Encoders[encType]; encType == Text && !ok {
			encType = JSON
		}

		if enc, ok := cmd.Encoders[encType]; ok {
			ee.SetEncoder(enc(req))
		} else if enc, ok := Encoders[encType]; ok {
			ee.SetEncoder(enc(req))
		} else {
			log.Errorf("unknown encoding %q, using json", encType)
			ee.SetEncoder(Encoders[JSON](req))
		}
	}

	if timeoutStr, ok := req.Options[TimeoutOpt]; ok {
		timeout, err := time.ParseDuration(timeoutStr.(string))
		if err != nil {
			return err
		}
		req.Context, _ = context.WithTimeout(req.Context, timeout)
	}

	if cmd.PreRun != nil {
		err = cmd.PreRun(req, env)
		if err != nil {
			return err
		}
	}

	// TODO(keks) use the reflect.Type as map key, not the string representation
	emitterType := EncodingType(reflect.TypeOf(re).String())
	if cmd.PostRun != nil && cmd.PostRun[emitterType] != nil {
		re = cmd.PostRun[emitterType](req, re)
	}

	defer func() {
		re.Close()
	}()
	defer func() {
		// catch panics in Run (esp. from re.SetError)
		if v := recover(); v != nil {
			// if they are errors
			if e, ok := v.(error); ok {
				// use them as return error
				err = re.Emit(cmdkit.Error{Message: e.Error(), Code: cmdkit.ErrNormal})
				if err != nil {
					log.Errorf("recovered from command error %q but failed emitting it: %q", e, err)
				}
			} else {
				// otherwise keep panicking.
				panic(v)
			}
		}

	}()
	cmd.Run(req, re, env)
	return nil
}

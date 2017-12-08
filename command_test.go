package cmds

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"gx/ipfs/QmUyfy4QSr3NXym4etEiRyxBLqqAeKHJuRdi8AACxg63fZ/go-ipfs-cmdkit"
)

// nopClose implements io.Close and does nothing
type nopCloser struct{}

func (c nopCloser) Close() error { return nil }

// newBufferResponseEmitter returns a ResponseEmitter that writes
// into a bytes.Buffer
func newBufferResponseEmitter() ResponseEmitter {
	buf := bytes.NewBuffer(nil)
	wc := writecloser{Writer: buf, Closer: nopCloser{}}
	return NewWriterResponseEmitter(wc, nil, Encoders[Text])
}

// noop does nothing and can be used as a noop Run function
func noop(req *Request, re ResponseEmitter, env interface{}) {
	return
}

// writecloser implements io.WriteCloser by embedding
// an io.Writer and an io.Closer
type writecloser struct {
	io.Writer
	io.Closer
}

// TestOptionValidation tests whether option type validation works
func TestOptionValidation(t *testing.T) {
	cmd := &Command{
		Options: []cmdkit.Option{
			cmdkit.IntOption("b", "beep", "enables beeper"),
			cmdkit.StringOption("B", "boop", "password for booper"),
		},
		Run: noop,
	}

	re := newBufferResponseEmitter()
	req, err := NewRequest(context.TODO(), nil, map[string]interface{}{
		"beep": true,
	}, nil, nil, cmd)
	if err == nil {
		t.Error("Should have failed (incorrect type)")
	}

	re = newBufferResponseEmitter()
	req, err = NewRequest(context.TODO(), nil, map[string]interface{}{
		"beep": 5,
	}, nil, nil, cmd)
	if err != nil {
		t.Error(err, "Should have passed")
	}
	err = cmd.Call(req, re, nil)
	if err != nil {
		t.Error(err, "Should have passed")
	}

	re = newBufferResponseEmitter()
	req, err = NewRequest(context.TODO(), nil, map[string]interface{}{
		"beep": 5,
		"boop": "test",
	}, nil, nil, cmd)
	if err != nil {
		t.Error("Should have passed")
	}

	err = cmd.Call(req, re, nil)
	if err != nil {
		t.Error("Should have passed")
	}

	re = newBufferResponseEmitter()
	req, err = NewRequest(context.TODO(), nil, map[string]interface{}{
		"b": 5,
		"B": "test",
	}, nil, nil, cmd)
	if err != nil {
		t.Error("Should have passed")
	}

	err = cmd.Call(req, re, nil)
	if err != nil {
		t.Error("Should have passed")
	}

	re = newBufferResponseEmitter()
	req, err = NewRequest(context.TODO(), nil, map[string]interface{}{
		"foo": 5,
	}, nil, nil, cmd)
	if err != nil {
		t.Error("Should have passed")
	}

	err = cmd.Call(req, re, nil)
	if err != nil {
		t.Error("Should have passed")
	}

	re = newBufferResponseEmitter()
	req, err = NewRequest(context.TODO(), nil, map[string]interface{}{
		cmdkit.EncShort: "json",
	}, nil, nil, cmd)
	if err != nil {
		t.Error("Should have passed")
	}

	err = cmd.Call(req, re, nil)
	if err != nil {
		t.Error("Should have passed")
	}

	re = newBufferResponseEmitter()
	req, err = NewRequest(context.TODO(), nil, map[string]interface{}{
		"b": "100",
	}, nil, nil, cmd)
	if err != nil {
		t.Error("Should have passed")
	}

	err = cmd.Call(req, re, nil)
	if err != nil {
		t.Error("Should have passed")
	}

	re = newBufferResponseEmitter()
	req, err = NewRequest(context.TODO(), nil, map[string]interface{}{
		"b": ":)",
	}, nil, nil, cmd)
	if err == nil {
		t.Error("Should have failed (string value not convertible to int)")
	}
	/*
		err = req.SetOptions(map[string]interface{}{
			"b": 100,
		})
		if err != nil {
			t.Error("Should have passed")
		}

		err = req.SetOptions(map[string]interface{}{
			"b": ":)",
		})
		if err == nil {
			t.Error("Should have failed (string value not convertible to int)")
		}
	*/
}

func TestRegistration(t *testing.T) {
	cmdA := &Command{
		Options: []cmdkit.Option{
			cmdkit.IntOption("beep", "number of beeps"),
		},
		Run: noop,
	}

	cmdB := &Command{
		Options: []cmdkit.Option{
			cmdkit.IntOption("beep", "number of beeps"),
		},
		Run: noop,
		Subcommands: map[string]*Command{
			"a": cmdA,
		},
	}

	path := []string{"a"}
	_, err := cmdB.GetOptions(path)
	if err == nil {
		t.Error("Should have failed (option name collision)")
	}
}

func TestOptionInheritance(t *testing.T) {
	cmd := &Command{
		Options: []cmdkit.Option{
			cmdkit.StringOption("foo", "f", "respect foo"),
		},
		Subcommands: map[string]*Command{
			"sub": &Command{},
		},
	}

	sub := cmd.Subcommand("sub")
	if len(sub.Options) != 1 {
		t.Error("expected one option, get %d", len(sub.Options))
	}

	names := sub.Options[0].Names()
	if len(names) != 2 || names[0] != "foo" || names[1] != "f" {
		t.Error("expected Option foo/f, got %v", sub.Options[0])
	}
}

func TestResolving(t *testing.T) {
	cmdC := &Command{}
	cmdB := &Command{
		Subcommands: map[string]*Command{
			"c": cmdC,
		},
	}
	cmdB2 := &Command{}
	cmdA := &Command{
		Subcommands: map[string]*Command{
			"b": cmdB,
			"B": cmdB2,
		},
	}
	cmd := &Command{
		Subcommands: map[string]*Command{
			"a": cmdA,
		},
	}

	cmds, err := cmd.Resolve([]string{"a", "b", "c"})
	if err != nil {
		t.Error(err)
	}
	// we can't test for equality because Resolve returns copies of the commands,
	// extended by the parent options
	if len(cmds) != 4 ||
		len(cmds[0].Subcommands) != 1 ||
		len(cmds[1].Subcommands) != 2 ||
		len(cmds[2].Subcommands) != 1 ||
		cmds[3].Subcommands != nil {
		t.Error("Returned command path is different than expected", cmds)
	}

	_, ok0 := cmds[0].Subcommands["a"]
	_, ok1 := cmds[1].Subcommands["b"]
	_, ok2 := cmds[1].Subcommands["B"]
	_, ok3 := cmds[2].Subcommands["c"]
	if !(ok0 && ok1 && ok2 && ok3) {
		t.Error("Returned command path is different than expected", cmds)
	}
}

func TestWalking(t *testing.T) {
	cmdA := &Command{
		Subcommands: map[string]*Command{
			"b": &Command{},
			"B": &Command{},
		},
	}
	i := 0
	cmdA.Walk(func(c *Command) {
		i = i + 1
	})
	if i != 3 {
		t.Error("Command tree walk didn't work, expected 3 got:", i)
	}
}

func TestHelpProcessing(t *testing.T) {
	cmdB := &Command{
		Helptext: cmdkit.HelpText{
			ShortDescription: "This is other short",
		},
	}
	cmdA := &Command{
		Helptext: cmdkit.HelpText{
			ShortDescription: "This is short",
		},
		Subcommands: map[string]*Command{
			"a": cmdB,
		},
	}
	cmdA.ProcessHelp()
	if len(cmdA.Helptext.LongDescription) == 0 {
		t.Error("LongDescription was not set on basis of ShortDescription")
	}
	if len(cmdB.Helptext.LongDescription) == 0 {
		t.Error("LongDescription was not set on basis of ShortDescription")
	}
}

type postRunTestCase struct {
	length      uint64
	err         *cmdkit.Error
	emit        []interface{}
	postRun     func(*Request, ResponseEmitter) ResponseEmitter
	next        []interface{}
	finalLength uint64
}

// TestPostRun tests whether commands with PostRun return the intended result
func TestPostRun(t *testing.T) {
	var testcases = []postRunTestCase{
		postRunTestCase{
			length:      3,
			err:         nil,
			emit:        []interface{}{7},
			finalLength: 4,
			next:        []interface{}{14},
			postRun: func(req *Request, re ResponseEmitter) ResponseEmitter {
				re_, res := NewChanResponsePair(req)

				go func() {
					defer re.Close()
					l := res.Length()
					re.SetLength(l + 1)

					for {
						v, err := res.Next()
						if err == io.EOF {
							return
						}
						if err != nil {
							re.SetError(err, cmdkit.ErrNormal)
							t.Fatal(err)
							return
						}

						i := v.(int)

						err = re.Emit(2 * i)
						if err != nil {
							re.SetError(err, cmdkit.ErrNormal)
							return
						}
					}
				}()

				return re_
			},
		},
	}

	for _, tc := range testcases {
		cmd := &Command{
			Run: func(req *Request, re ResponseEmitter, env interface{}) {
				re.SetLength(tc.length)

				for _, v := range tc.emit {
					err := re.Emit(v)
					if err != nil {
						t.Fatal(err)
					}
				}
				err := re.Close()
				if err != nil {
					t.Fatal(err)
				}
			},
			PostRun: PostRunMap{
				CLI: tc.postRun,
			},
		}

		req, err := NewRequest(context.TODO(), nil, map[string]interface{}{
			cmdkit.EncShort: CLI,
		}, nil, nil, cmd)
		if err != nil {
			t.Fatal(err)
		}

		opts := req.Options
		if opts == nil {
			t.Fatal("req.Options() is nil")
		}

		encTypeIface := opts[cmdkit.EncShort]
		if encTypeIface == nil {
			t.Fatal("req.Options()[cmdkit.EncShort] is nil")
		}

		encType := EncodingType(encTypeIface.(string))
		if encType == "" {
			t.Fatal("no encoding type")
		}

		if encType != CLI {
			t.Fatal("wrong encoding type")
		}

		re, res := NewChanResponsePair(req)
		re = cmd.PostRun[encType](req, re)

		err = cmd.Call(req, re, nil)
		if err != nil {
			t.Fatal(err)
		}

		l := res.Length()
		if l != tc.finalLength {
			t.Fatal("wrong final length")
		}

		for _, x := range tc.next {
			ch := make(chan interface{})

			go func() {
				v, err := res.Next()
				if err != nil {
					close(ch)
					t.Fatal(err)
				}

				ch <- v
			}()

			select {
			case v, ok := <-ch:
				if !ok {
					t.Fatal("error checking all next values - channel closed")
				}
				if x != v {
					t.Fatalf("final check of emitted values failed. got %v but expected %v", v, x)
				}
			case <-time.After(50 * time.Millisecond):
				t.Fatal("too few values in next")
			}
		}

		_, err = res.Next()
		if err != io.EOF {
			t.Fatal("expected EOF, got", err)
		}
	}
}

func TestCancel(t *testing.T) {
	wait := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	req, err := NewRequest(ctx, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	re, res := NewChanResponsePair(req)

	go func() {
		err := re.Emit("abc")
		if err != context.Canceled {
			t.Fatalf("re:  expected context.Canceled but got %v", err)
		}
		t.Log("re.Emit err:", err)
		re.Close()
		close(wait)
	}()

	cancel()

	_, err = res.Next()
	if err != context.Canceled {
		t.Fatalf("res: expected context.Canceled but got %v", err)
	}
	t.Log("res.Emit err:", err)
	<-wait
}

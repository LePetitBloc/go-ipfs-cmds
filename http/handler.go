package http

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"

	context "context"
	"github.com/ipfs/go-ipfs/repo/config"
	cors "github.com/rs/cors"

	cmds "github.com/ipfs/go-ipfs-cmds"

	logging "github.com/ipfs/go-log"
)

var log = logging.Logger("cmds/http")

// the internal handler for the API
type internalHandler struct {
	root *cmds.Command
	cfg  *ServerConfig
	env  interface{}
}

// The Handler struct is funny because we want to wrap our internal handler
// with CORS while keeping our fields.
type Handler struct {
	internalHandler
	corsHandler http.Handler
}

var (
	ErrNotFound           = errors.New("404 page not found")
	errApiVersionMismatch = errors.New("api version mismatch")
)

const (
	StreamErrHeader          = "X-Stream-Error"
	streamHeader             = "X-Stream-Output"
	channelHeader            = "X-Chunked-Output"
	extraContentLengthHeader = "X-Content-Length"
	uaHeader                 = "User-Agent"
	contentTypeHeader        = "Content-Type"
	contentDispHeader        = "Content-Disposition"
	transferEncodingHeader   = "Transfer-Encoding"
	applicationJson          = "application/json"
	applicationOctetStream   = "application/octet-stream"
	plainText                = "text/plain"
	originHeader             = "origin"
)

var AllowedExposedHeadersArr = []string{streamHeader, channelHeader, extraContentLengthHeader}
var AllowedExposedHeaders = strings.Join(AllowedExposedHeadersArr, ", ")

const (
	ACAOrigin      = "Access-Control-Allow-Origin"
	ACAMethods     = "Access-Control-Allow-Methods"
	ACACredentials = "Access-Control-Allow-Credentials"
)

var mimeTypes = map[cmds.EncodingType]string{
	cmds.Protobuf: "application/protobuf",
	cmds.JSON:     "application/json",
	cmds.XML:      "application/xml",
	cmds.Text:     "text/plain",
}

type ServerConfig struct {
	// Headers is an optional map of headers that is written out.
	Headers map[string][]string

	// corsOpts is a set of options for CORS headers.
	corsOpts *cors.Options

	// corsOptsRWMutex is a RWMutex for read/write CORSOpts
	corsOptsRWMutex sync.RWMutex
}

func skipAPIHeader(h string) bool {
	switch h {
	case "Access-Control-Allow-Origin":
		return true
	case "Access-Control-Allow-Methods":
		return true
	case "Access-Control-Allow-Credentials":
		return true
	default:
		return false
	}
}

func NewHandler(env interface{}, root *cmds.Command, cfg *ServerConfig) http.Handler {
	if cfg == nil {
		panic("must provide a valid ServerConfig")
	}

	// Wrap the internal handler with CORS handling-middleware.
	// Create a handler for the API.
	internal := internalHandler{
		env:  env,
		root: root,
		cfg:  cfg,
	}
	c := cors.New(*cfg.corsOpts)
	return &Handler{internal, c.Handler(internal)}
}

func (i Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Call the CORS handler which wraps the internal handler.
	i.corsHandler.ServeHTTP(w, r)
}

type requestAdder interface {
	AddRequest(*cmds.Request) func()
}

type contexter interface {
	Context() context.Context
}

func (i internalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Debug("incoming API request: ", r.URL)

	defer func() {
		if r := recover(); r != nil {
			log.Error("a panic has occurred in the commands handler!")
			log.Error(r)
			log.Errorf("stack trace:\n%s", debug.Stack())
		}
	}()

	var ctx context.Context
	if ctxer, ok := i.env.(contexter); ok {
		ctx, cancel := context.WithCancel(ctxer.Context())
		defer cancel()
		if cn, ok := w.(http.CloseNotifier); ok {
			clientGone := cn.CloseNotify()
			go func() {
				select {
				case <-clientGone:
				case <-ctx.Done():
				}
				cancel()
			}()
		}
	}

	if !allowOrigin(r, i.cfg) || !allowReferer(r, i.cfg) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("403 - Forbidden"))
		log.Warningf("API blocked request to %s. (possible CSRF)", r.URL)
		return
	}

	req, err := parseRequest(ctx, r, i.root)
	if err != nil {
		if err == ErrNotFound {
			w.WriteHeader(http.StatusNotFound)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		w.Write([]byte(err.Error()))
		return
	}

	if reqAdder, ok := i.env.(requestAdder); ok {
		done := reqAdder.AddRequest(req)
		defer done()
	}

	req.Context = ctx

	// set user's headers first.
	for k, v := range i.cfg.Headers {
		if !skipAPIHeader(k) {
			w.Header()[k] = v
		}
	}

	re := NewResponseEmitter(w, r.Method, req)

	// call the command
	err = i.root.Call(req, re, i.env)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	return
}

func sanitizedErrStr(err error) string {
	s := err.Error()
	s = strings.Split(s, "\n")[0]
	s = strings.Split(s, "\r")[0]
	return s
}

func NewServerConfig() *ServerConfig {
	cfg := new(ServerConfig)
	cfg.corsOpts = new(cors.Options)
	return cfg
}

func (cfg ServerConfig) AllowedOrigins() []string {
	cfg.corsOptsRWMutex.RLock()
	defer cfg.corsOptsRWMutex.RUnlock()
	return cfg.corsOpts.AllowedOrigins
}

func (cfg *ServerConfig) SetAllowedOrigins(origins ...string) {
	cfg.corsOptsRWMutex.Lock()
	defer cfg.corsOptsRWMutex.Unlock()
	o := make([]string, len(origins))
	copy(o, origins)
	cfg.corsOpts.AllowedOrigins = o
}

func (cfg *ServerConfig) AppendAllowedOrigins(origins ...string) {
	cfg.corsOptsRWMutex.Lock()
	defer cfg.corsOptsRWMutex.Unlock()
	cfg.corsOpts.AllowedOrigins = append(cfg.corsOpts.AllowedOrigins, origins...)
}

func (cfg ServerConfig) AllowedMethods() []string {
	cfg.corsOptsRWMutex.RLock()
	defer cfg.corsOptsRWMutex.RUnlock()
	return []string(cfg.corsOpts.AllowedMethods)
}

func (cfg *ServerConfig) SetAllowedMethods(methods ...string) {
	cfg.corsOptsRWMutex.Lock()
	defer cfg.corsOptsRWMutex.Unlock()
	if cfg.corsOpts == nil {
		cfg.corsOpts = new(cors.Options)
	}
	cfg.corsOpts.AllowedMethods = methods
}

func (cfg *ServerConfig) SetAllowCredentials(flag bool) {
	cfg.corsOptsRWMutex.Lock()
	defer cfg.corsOptsRWMutex.Unlock()
	cfg.corsOpts.AllowCredentials = flag
}

// allowOrigin just stops the request if the origin is not allowed.
// the CORS middleware apparently does not do this for us...
func allowOrigin(r *http.Request, cfg *ServerConfig) bool {
	origin := r.Header.Get("Origin")

	// curl, or ipfs shell, typing it in manually, or clicking link
	// NOT in a browser. this opens up a hole. we should close it,
	// but right now it would break things. TODO
	if origin == "" {
		return true
	}
	origins := cfg.AllowedOrigins()
	for _, o := range origins {
		if o == "*" { // ok! you asked for it!
			return true
		}

		if o == origin { // allowed explicitly
			return true
		}
	}

	return false
}

// allowReferer this is here to prevent some CSRF attacks that
// the API would be vulnerable to. We check that the Referer
// is allowed by CORS Origin (origins and referrers here will
// work similarly in the normla uses of the API).
// See discussion at https://github.com/ipfs/go-ipfs/issues/1532
func allowReferer(r *http.Request, cfg *ServerConfig) bool {
	referer := r.Referer()

	// curl, or ipfs shell, typing it in manually, or clicking link
	// NOT in a browser. this opens up a hole. we should close it,
	// but right now it would break things. TODO
	if referer == "" {
		return true
	}

	u, err := url.Parse(referer)
	if err != nil {
		// bad referer. but there _is_ something, so bail.
		// debug because referer comes straight from the client. dont want to
		// let people DOS by putting a huge referer that gets stored in log files.
		return false
	}
	origin := u.Scheme + "://" + u.Host

	// check CORS ACAOs and pretend Referer works like an origin.
	// this is valid for many (most?) sane uses of the API in
	// other applications, and will have the desired effect.
	origins := cfg.AllowedOrigins()
	for _, o := range origins {
		if o == "*" { // ok! you asked for it!
			return true
		}

		// referer is allowed explicitly
		if o == origin {
			return true
		}
	}

	return false
}

// apiVersionMatches checks whether the api client is running the
// same version of go-ipfs. for now, only the exact same version of
// client + server work. In the future, we should use semver for
// proper API versioning! \o/
func apiVersionMatches(r *http.Request) error {
	clientVersion := r.UserAgent()
	// skips check if client is not go-ipfs
	if clientVersion == "" || !strings.Contains(clientVersion, "/go-ipfs/") {
		return nil
	}

	daemonVersion := config.ApiVersion
	if daemonVersion != clientVersion {
		return fmt.Errorf("%s (%s != %s)", errApiVersionMismatch, daemonVersion, clientVersion)
	}
	return nil
}

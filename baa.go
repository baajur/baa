// Package baa provider a fast & simple Go web framework, routing, middleware, dependency injection, http context.
/*
app := baa.Classic()
app.Get("/", function(c *baa.Context) error {
    c.String(200, "Hello World!")
    return nil
})
app.Run(":8001")
*/
package baa

import (
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

const (
	DEV  = "development"
	PROD = "production"
	TEST = "test"
)

// Baa provlider an application
type Baa struct {
	name             string
	router           *Router
	logger           Logger
	debug            bool
	httpErrorHandler HTTPErrorHandler
	middleware       []MiddlewareFunc
	di               *DI
	pool             sync.Pool
	render           Renderer
}

// Middleware ...
type Middleware interface{}

// MiddlewareFunc ...
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// Handler ...
type Handler interface{}

// HandlerFunc context handler
type HandlerFunc func(*Context) error

// HTTPErrorHandler is a centralized HTTP error handler.
type HTTPErrorHandler func(error, *Context)

// HTTPError represents an error that occured while handling a request.
type HTTPError struct {
	code    int
	message string
}

// default application for baa
var _defaultAPP *Baa

func init() {
	_defaultAPP = Classic()
}

// App return the default baa instance
func App() *Baa {
	return _defaultAPP
}

// Classic create a baa application with default config.
func Classic() *Baa {
	b := New()
	b.SetRender(NewRender())
	b.SetHTTPErrorHandler(b.DefaultHTTPErrorHandler)
	return b
}

// New create a baa application without any config.
func New() *Baa {
	b := new(Baa)
	b.middleware = make([]MiddlewareFunc, 0)
	b.pool.New = func() interface{} {
		return NewContext(nil, nil, nil)
	}
	b.SetLogger(log.New(os.Stderr, "[Baa]", log.LstdFlags))
	b.SetDIer(NewDI())
	b.SetRouter(NewRouter(b))
	return b
}

// Server returns the internal *http.Server.
func (b *Baa) Server(addr string) *http.Server {
	s := &http.Server{Addr: addr}
	return s
}

// Run runs a server.
func (b *Baa) Run(addr string) {
	b.run(b.Server(addr))
}

// RunTLS runs a server with TLS configuration.
func (b *Baa) RunTLS(addr, certfile, keyfile string) {
	b.run(b.Server(addr), certfile, keyfile)
}

// RunServer runs a custom server.
func (b *Baa) RunServer(s *http.Server) {
	b.run(s)
}

// RunTLSServer runs a custom server with TLS configuration.
func (b *Baa) RunTLSServer(s *http.Server, crtFile, keyFile string) {
	b.run(s, crtFile, keyFile)
}

func (b *Baa) run(s *http.Server, files ...string) {
	s.Handler = b
	if len(files) == 0 {
		b.logger.Fatal(s.ListenAndServe())
	} else if len(files) == 2 {
		b.logger.Fatal(s.ListenAndServeTLS(files[0], files[1]))
	} else {
		b.logger.Fatal("invalid TLS configuration")
	}
}

func (b *Baa) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c := b.pool.Get().(*Context)
	defer b.pool.Put(c)
	c.reset(w, r, b)

	var h HandlerFunc
	route := b.router.Match(r.Method, r.URL.Path)
	if route == nil {
		// notFound
		h = b.router.GetNotFoundHandler()
		if h == nil {
			h = func(c *Context) error {
				http.NotFound(c.Resp, c.Req)
				return nil
			}
		}
	} else {
		h = route.handle
	}

	// Chain middleware with handler in the end
	for i := len(b.middleware) - 1; i >= 0; i-- {
		h = b.middleware[i](h)
	}

	// Execute chain
	if err := h(c); err != nil {
		b.httpErrorHandler(err, c)
	}
}

// SetDebug set baa debug
func (b *Baa) SetDebug(v bool) {
	b.debug = v
}

// SetDIer registers a Baa.DI
func (b *Baa) SetDIer(di *DI) {
	b.di = di
}

// SetLogger registers a Baa.Logger
func (b *Baa) SetLogger(logger Logger) {
	b.logger = logger
}

// GetLogger ...
func (b *Baa) GetLogger() Logger {
	return b.logger
}

// SetRender registers a Baa.Renderer
func (b *Baa) SetRender(r Renderer) {
	b.render = r
}

// SetRouter registers a Baa.Router
func (b *Baa) SetRouter(r *Router) {
	b.router = r
}

// SetHTTPErrorHandler registers a custom Baa.HTTPErrorHandler.
func (b *Baa) SetHTTPErrorHandler(h HTTPErrorHandler) {
	b.httpErrorHandler = h
}

// DefaultHTTPErrorHandler invokes the default HTTP error handler.
func (b *Baa) DefaultHTTPErrorHandler(err error, c *Context) {
	code := http.StatusInternalServerError
	msg := http.StatusText(code)
	if he, ok := err.(*HTTPError); ok {
		code = he.code
		msg = he.message
	}
	if b.debug {
		msg = err.Error()
	}
	http.Error(c.Resp, msg, code)
}

// Use registers a middleware
func (b *Baa) Use(m Middleware) {
	b.middleware = append(b.middleware, wrapMiddleware(m))
}

// SetDI registers a dependency injection
func (b *Baa) SetDI(name string, h interface{}) {
	b.di.Set(name, h)
}

// GetDI fetch a registered dependency injection
func (b *Baa) GetDI(name string) interface{} {
	return b.di.Get(name)
}

// SetAutoHead sets the value who determines whether add HEAD method automatically
// when GET method is added. Combo router will not be affected by this value.
func (b *Baa) SetAutoHead(v bool) {
	b.router.autoHead = v
}

// Route is a shortcut for same handlers but different HTTP methods.
//
// Example:
// 		baa.route("/", "GET,POST", h)
func (b *Baa) Route(pattern, methods string, h ...Handler) *Route {
	var rs *Route
	for _, m := range strings.Split(methods, ",") {
		rs = b.router.add(strings.TrimSpace(m), pattern, h)
	}
	return rs
}

// Group registers a list of same prefix route
func (b *Baa) Group(pattern string, fn func(), h ...Handler) {

}

// Get is a shortcut for b.router.handle("GET", pattern, handlers)
func (b *Baa) Get(pattern string, h ...Handler) *Route {
	rs := b.router.add("GET", pattern, h)
	if b.router.autoHead {
		b.Head(pattern, h...)
	}
	return rs
}

// Patch is a shortcut for b.router.handle("PATCH", pattern, handlers)
func (b *Baa) Patch(pattern string, h ...Handler) *Route {
	return b.router.add("PATCH", pattern, h)
}

// Post is a shortcut for b.router.handle("POST", pattern, handlers)
func (b *Baa) Post(pattern string, h ...Handler) *Route {
	return b.router.add("POST", pattern, h)
}

// Put is a shortcut for b.router.handle("PUT", pattern, handlers)
func (b *Baa) Put(pattern string, h ...Handler) *Route {
	return b.router.add("PUT", pattern, h)
}

// Delete is a shortcut for b.router.handle("DELETE", pattern, handlers)
func (b *Baa) Delete(pattern string, h ...Handler) *Route {
	return b.router.add("DELETE", pattern, h)
}

// Options is a shortcut for b.router.handle("OPTIONS", pattern, handlers)
func (b *Baa) Options(pattern string, h ...Handler) *Route {
	return b.router.add("OPTIONS", pattern, h)
}

// Head is a shortcut for b.router.handle("HEAD", pattern, handlers)
func (b *Baa) Head(pattern string, h ...Handler) *Route {
	return b.router.add("HEAD", pattern, h)
}

// Any is a shortcut for b.router.handle("*", pattern, handlers)
func (b *Baa) Any(pattern string, h ...Handler) *Route {
	return b.router.add("*", pattern, h)
}

// NotFound set 404 router
func (b *Baa) NotFound(h Handler) {
	b.router.NotFound(h)
}

// NewHTTPError creates a new HTTPError instance.
func NewHTTPError(code int, msg ...string) *HTTPError {
	e := &HTTPError{code: code, message: http.StatusText(code)}
	if len(msg) > 0 {
		m := msg[0]
		e.message = m
	}
	return e
}

// SetCode sets code.
func (e *HTTPError) SetCode(code int) {
	e.code = code
}

// Code returns code.
func (e *HTTPError) Code() int {
	return e.code
}

// Error returns message.
func (e *HTTPError) Error() string {
	return e.message
}

// wrapMiddleware wraps middleware.
func wrapMiddleware(m Middleware) MiddlewareFunc {
	switch m := m.(type) {
	case MiddlewareFunc:
		return m
	case func(HandlerFunc) HandlerFunc:
		return m
	case HandlerFunc:
		return wrapHandlerFuncMiddleware(m)
	case func(*Context) error:
		return wrapHandlerFuncMiddleware(m)
	case func(http.Handler) http.Handler:
		return func(h HandlerFunc) HandlerFunc {
			return func(c *Context) (err error) {
				m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					c.Resp = w
					c.Req = r
					err = h(c)
				})).ServeHTTP(c.Resp, c.Req)
				return
			}
		}
	case http.Handler:
		return wrapHTTPHandlerFuncMiddleware(m.ServeHTTP)
	case func(http.ResponseWriter, *http.Request):
		return wrapHTTPHandlerFuncMiddleware(m)
	default:
		panic("unknown middleware")
	}
}

// wrapHandlerFuncMiddleware wraps HandlerFunc middleware.
func wrapHandlerFuncMiddleware(m HandlerFunc) MiddlewareFunc {
	return func(h HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			if err := m(c); err != nil {
				return err
			}
			return h(c)
		}
	}
}

// wrapHTTPHandlerFuncMiddleware wraps http.HandlerFunc middleware.
func wrapHTTPHandlerFuncMiddleware(m http.HandlerFunc) MiddlewareFunc {
	return func(h HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			m.ServeHTTP(c.Resp, c.Req)
			return h(c)
		}
	}
}

// wrapHandler wraps handler.
func wrapHandler(h Handler) HandlerFunc {
	switch h := h.(type) {
	case HandlerFunc:
		return h
	case func(*Context) error:
		return h
	case http.Handler, http.HandlerFunc:
		return func(c *Context) error {
			h.(http.Handler).ServeHTTP(c.Resp, c.Req)
			return nil
		}
	case func(http.ResponseWriter, *http.Request):
		return func(c *Context) error {
			h(c.Resp, c.Req)
			return nil
		}
	default:
		panic("unknown handler")
	}
}
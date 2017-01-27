// Copyright (c) Jeevanandam M. (https://github.com/jeevatkm)
// go-aah/aah source code and usage is governed by a MIT style
// license that can be found in the LICENSE file.

package aah

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"aahframework.org/aah/ahttp"
	"aahframework.org/aah/aruntime"
	"aahframework.org/aah/reply"
	"aahframework.org/aah/router"
	"aahframework.org/log"
	"aahframework.org/pool"
)

var errFileNotFound = errors.New("file not found")

type (
	// Engine is the aah framework application server handler for request and response.
	// Implements `http.Handler` interface.
	engine struct {
		cPool *pool.Pool
		rPool *pool.Pool
		bPool *pool.Pool
	}
)

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// Engine methods
//___________________________________

// ServeHTTP method implementation of http.Handler interface.
func (e *engine) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	c, r := e.getController(), e.getRequest()
	defer func() {
		c.close()
		e.putRequest(r)
		e.putController(c)
	}()

	c.Req = ahttp.ParseRequest(req, r)
	c.Res = ahttp.WrapResponseWriter(w)
	c.reply = reply.NewReply()

	// recovery handling
	defer e.handleRecovery(c)

	// TODO Detailed server access log to separate file later on
	log.Debugf("Request %s", c.Req.Path)

	// set defaults when actual value not found
	e.setDefaults(c)

	// Middlewares
	e.executeMiddlewares(c)

	// Write response
	e.writeResponse(c)
}

// handleRecovery handles application panics and recovers from it. Panic gets
// translated into HTTP Internal Server Error (Status 500).
func (e *engine) handleRecovery(c *Controller) {
	if r := recover(); r != nil {
		log.Errorf("Internal Server Error on %s", c.Req.Path)

		st := aruntime.NewStacktrace(r, AppConfig())
		buf := e.getBuffer()
		defer e.putBuffer(buf)

		st.Print(buf)
		log.Error(buf.String())

		if AppProfile() != "prod" { // detailed error info
			// TODO design server error page with stack trace info
			c.Reply().Status(http.StatusInternalServerError).Text("Internal Server Error: %s", buf.String())
			e.writeResponse(c)
			return
		}

		// For "prod", detailed information gets logged
		c.Reply().Status(http.StatusInternalServerError).Text("Internal Server Error")
		e.writeResponse(c)
	}
}

// setDefaults method sets default value based on aah app configuration
// when actual value is not found.
func (e *engine) setDefaults(c *Controller) {
	if c.Req.Locale == nil {
		c.Req.Locale = ahttp.NewLocale(appConfig.StringDefault("i18n.default", "en"))
	}
}

// executeMiddlewares method executes the configured middlewares.
func (e *engine) executeMiddlewares(c *Controller) {
	mwChain[0].Next(c)
}

// writeResponse method writes the response on the wire based on `Reply` values.
func (e *engine) writeResponse(c *Controller) {
	reply := c.Reply()
	buf := e.getBuffer()
	defer e.putBuffer(buf)

	// Render and detect the errors earlier, framework can write error info
	// without messing with response.
	// HTTP Body
	if reply.Rdr != nil {
		if err := reply.Rdr.Render(buf); err != nil {
			log.Error("Render error: ", err)
			c.Res.WriteHeader(http.StatusInternalServerError)
			_, _ = c.Res.Write([]byte("Render error: " + err.Error() + "\n"))
			return
		}
	}

	// HTTP headers
	for k, v := range reply.Hdr {
		for _, vv := range v {
			c.Res.Header().Add(k, vv)
		}
	}

	// ContentType
	c.Res.Header().Set(ahttp.HeaderContentType, reply.ContType)

	// HTTP status
	if reply.IsStatusSet() {
		c.Res.WriteHeader(reply.Code)
	} else {
		c.Res.WriteHeader(http.StatusOK)
	}

	// Write it on the wire
	_, _ = buf.WriteTo(c.Res)
}

// defaultContentType method returns the Content-Type based on 'render.default'
// config from aah.conf
func defaultContentType() *ahttp.ContentType {
	cfgValue := AppConfig().StringDefault("render.default", "")
	switch cfgValue {
	case "html":
		return ahttp.ContentTypeHTML
	case "json":
		return ahttp.ContentTypeJSON
	case "xml":
		return ahttp.ContentTypeXML
	case "text":
		return ahttp.ContentTypePlainText
	default:
		return ahttp.ContentTypeOctetStream
	}
}

// getController method gets controller from pool
func (e *engine) getController() *Controller {
	return e.cPool.Get().(*Controller)
}

// getRequest method gets request from pool
func (e *engine) getRequest() *ahttp.Request {
	return e.rPool.Get().(*ahttp.Request)
}

// putController method puts controller back to pool
func (e *engine) putController(c *Controller) {
	c.Reset()
	e.cPool.Put(c)
}

// putRequest method puts request back to pool
func (e *engine) putRequest(r *ahttp.Request) {
	r.Reset()
	e.rPool.Put(r)
}

// getBuffer method gets buffer from pool
func (e *engine) getBuffer() *bytes.Buffer {
	return e.bPool.Get().(*bytes.Buffer)
}

// putBPool puts buffer into pool
func (e *engine) putBuffer(b *bytes.Buffer) {
	b.Reset()
	e.bPool.Put(b)
}

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// Unexported methods
//___________________________________

// serveStatic method static file/directory delivery.
func serveStatic(c *Controller, route *router.Route, pathParams *router.PathParams) error {

	fmt.Println("Static route:", route, pathParams)

	// TODO static serve implementation

	return errFileNotFound
}

// handleNotFound method is used for 1. route action not found, 2. route is
// not found and 3. static file/directory.
func handleNotFound(c *Controller, domain *router.Domain, isStatic bool) {
	if domain.NotFoundRoute == nil {
		c.Reply().NotFound().Text("404 Not Found")
		return
	}

	if err := c.setTarget(domain.NotFoundRoute); err != errTargetNotFound {
		target := reflect.ValueOf(c.target)
		if notFoundAction := target.MethodByName(c.action.Name); notFoundAction.IsValid() {
			log.Debugf("Calling custom defined not-found action: %s.%s", c.controller, c.action.Name)
			notFoundAction.Call([]reflect.Value{reflect.ValueOf(isStatic)})
		} else {
			c.Reply().NotFound().Text("404 Not Found")
		}
	}
}

// Redirect method redirects request to given URL.
func redirectTrailingSlash(c *Controller) {
	code := http.StatusMovedPermanently
	if c.Req.Method != ahttp.MethodGet {
		code = http.StatusTemporaryRedirect
	}

	path := c.Req.Path
	req := c.Req.Raw
	if len(path) > 1 && path[len(path)-1] == '/' {
		req.URL.Path = path[:len(path)-1]
	} else {
		req.URL.Path = path + "/"
	}

	log.Debugf("RedirectTrailingSlash: %d, %s ==> %s", code, path, req.URL.String())
	http.Redirect(c.Res, req, req.URL.String(), code)
}

func newEngine() *engine {
	// TODO provide config for pool size
	return &engine{
		cPool: pool.NewPool(150, func() interface{} {
			return &Controller{}
		}),
		rPool: pool.NewPool(150, func() interface{} {
			return &ahttp.Request{}
		}),
		bPool: pool.NewPool(50, func() interface{} {
			return &bytes.Buffer{}
		}),
	}
}
package testutils

import (
	"aahframe.work"
	"aahframe.work/ahttp"
	"aahframe.work/log"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type TestServer struct {
	URL    string
	app    *aah.Application
	server *httptest.Server
}

func NewTestServer(t *testing.T, importPath string) *TestServer {
	ts := &TestServer{
		app: newTestApp(t, importPath),
	}

	ts.server = httptest.NewServer(ts.app)
	ts.URL = ts.server.URL

	// Manually do it here here, for aah CLI test no issue `aah test` :)
	ts.manualInit()

	ts.DiscardLog()

	return ts
}

func (ts *TestServer) Close() {
	ts.server.Close()
}

func (ts *TestServer) DiscardLog() {
	ts.app.Log().(*log.Logger).SetWriter(ioutil.Discard)
}

func (ts *TestServer) LogToStdout() {
	ts.app.Log().(*log.Logger).SetWriter(os.Stdout)
}

func (ts *TestServer) manualInit() {
	// adding middlewares
	ts.app.HTTPEngine().Middlewares(
		aah.RouteMiddleware,
		aah.CORSMiddleware,
		aah.BindMiddleware,
		aah.AntiCSRFMiddleware,
		aah.AuthcAuthzMiddleware,
		aah.ActionMiddleware,
	)
}

func newTestApp(t *testing.T, importPath string) *aah.Application {
	a := aah.NewApp()
	a.SetBuildInfo(&aah.BuildInfo{
		BinaryName: filepath.Base(importPath),
		Timestamp:  time.Now().Format(time.RFC3339),
		Version:    "1.0.0",
	})

	a.VFS().AddMount(a.VirtualBaseDir(), importPath)
	return a
}

func NewContext(w http.ResponseWriter, r *http.Request) *aah.Context {
	ctx := &aah.Context{}

	if r != nil {
		ctx.Req = ahttp.AcquireRequest(r)
	}

	if w != nil {
		ctx.Res = ahttp.AcquireResponseWriter(w)
	}

	return ctx
}

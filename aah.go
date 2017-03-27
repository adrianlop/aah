// Copyright (c) Jeevanandam M. (https://github.com/jeevatkm)
// go-aah/aah source code and usage is governed by a MIT style
// license that can be found in the LICENSE file.

// Package aah is A scalable, performant, rapid development Web framework for Go
// https://aahframework.org
package aah

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aahframework.org/aruntime.v0"
	"aahframework.org/atemplate.v0"
	"aahframework.org/config.v0"
	"aahframework.org/essentials.v0"
	"aahframework.org/log.v0"
)

// Version no. of aah framework
const Version = "0.4"

// aah application variables
var (
	appName               string
	appImportPath         string
	appProfile            string
	appBaseDir            string
	appIsPackaged         bool
	appHTTPReadTimeout    time.Duration
	appHTTPWriteTimeout   time.Duration
	appHTTPMaxHdrBytes    int
	appSSLCert            string
	appSSLKey             string
	appMultipartMaxMemory int64
	appPID                int
	appInitialized        bool
	appBuildInfo          *BuildInfo
	appEngine             *engine

	appDefaultProfile        = "dev"
	appProfileProd           = "prod"
	appProfilePrefix         = "env."
	appDefaultHTTPPort       = 8080
	appDefaultDateFormat     = "2006-01-02"
	appDefaultDateTimeFormat = "2006-01-02 15:04:05"
	appModeWeb               = "web"

	goPath   string
	goSrcDir string
)

// BuildInfo holds the aah application build information; such as BinaryName,
// Version and Date.
type BuildInfo struct {
	BinaryName string
	Version    string
	Date       string
}

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// Global methods
//___________________________________

// AppName method returns aah application name from app config otherwise app name
// of the base directory.
func AppName() string {
	return appName
}

// AppProfile returns aah application configuration profile name
// For e.g.: dev, prod, etc. Default is `dev`
func AppProfile() string {
	return appProfile
}

// AppBaseDir method returns the application base or binary current directory
// 	For e.g.:
// 		$GOPATH/src/github.com/user/myproject
// 		<app/binary/path/base/directory>
func AppBaseDir() string {
	if appIsPackaged {
		wd, _ := os.Getwd()
		if strings.HasSuffix(wd, "/bin") {
			wd = wd[:len(wd)-4]
		}
		appBaseDir = wd
	}
	return appBaseDir
}

// AppImportPath method returns the application Go import path.
func AppImportPath() string {
	return appImportPath
}

// AppMode method returns aah application mode. Default is "web" For e.g.: web or api
func AppMode() string {
	return AppConfig().StringDefault("mode", appModeWeb)
}

// AppHTTPAddress method returns aah application HTTP address otherwise empty string
func AppHTTPAddress() string {
	return AppConfig().StringDefault("server.address", "")
}

// AppHTTPPort method returns aah application HTTP port number if available
// otherwise returns default port number 8080.
func AppHTTPPort() int {
	return AppConfig().IntDefault("server.port", appDefaultHTTPPort)
}

// AppDateFormat method returns aah application date format
func AppDateFormat() string {
	return AppConfig().StringDefault("format.date", appDefaultDateFormat)
}

// AppDateTimeFormat method returns aah application date format
func AppDateTimeFormat() string {
	return AppConfig().StringDefault("format.datetime", appDefaultDateTimeFormat)
}

// AppBuildInfo method return user application version no.
func AppBuildInfo() *BuildInfo {
	return appBuildInfo
}

// AllAppProfiles method returns all the aah application environment profile names.
func AllAppProfiles() []string {
	var profiles []string

	for _, v := range AppConfig().KeysByPath("env") {
		if v == "default" {
			continue
		}
		profiles = append(profiles, v)
	}

	return profiles
}

// IsSSLEnabled method returns true if aah application is enabled with SSL
// otherwise false.
func IsSSLEnabled() bool {
	return AppConfig().BoolDefault("server.ssl.enable", false)
}

// AddTemplateFunc method adds template func map into view engine.
func AddTemplateFunc(funcs template.FuncMap) {
	atemplate.AddTemplateFunc(funcs)
}

// SetAppProfile method sets given profile as current aah application profile.
//		For Example:
//
//		aah.SetAppProfile("prod")
func SetAppProfile(profile string) error {
	if err := AppConfig().SetProfile(appProfilePrefix + profile); err != nil {
		return err
	}

	appProfile = profile
	return nil
}

// SetAppBuildInfo method sets the user application build info into aah instance.
func SetAppBuildInfo(bi *BuildInfo) {
	appBuildInfo = bi
}

// Init method initializes `aah` application, if anything goes wrong during
// initialize process, it will log it as fatal msg and exit.
func Init(importPath string) {
	defer aahRecover()

	logAsFatal(initPath(importPath))
	logAsFatal(initConfig(appConfigDir()))

	initInternal()
}

// Start method starts the HTTP server based on aah config "server.*".
func Start() {
	defer aahRecover()

	if !appInitialized {
		log.Fatal("aah application is not initialized, call `aah.Init` before the `aah.Start`.")
	}

	log.Infof("App Name: %v", AppName())
	log.Infof("App Profile: %v", AppProfile())
	log.Infof("App Mode: %v", AppMode())
	log.Debugf("App i18n Locales: %v", strings.Join(AppI18n().Locales(), ", "))
	log.Debugf("App Route Domains: %v", strings.Join(AppRouter().DomainAddresses(), ", "))

	// Publish `OnStart` event
	AppEventStore().sortAndPublishSync(&Event{Name: EventOnStart})

	address := AppHTTPAddress()
	appEngine = newEngine()
	server := &http.Server{
		Handler:        appEngine,
		ReadTimeout:    appHTTPReadTimeout,
		WriteTimeout:   appHTTPWriteTimeout,
		MaxHeaderBytes: appHTTPMaxHdrBytes,
	}

	server.SetKeepAlivesEnabled(AppConfig().BoolDefault("server.keep_alive", true))

	writePID(AppName(), AppBaseDir(), AppConfig())

	// Unix Socket
	if strings.HasPrefix(address, "unix") {
		log.Infof("Listening and serving HTTP on %v", address)

		sockFile := address[5:]
		if err := os.Remove(sockFile); !os.IsNotExist(err) {
			logAsFatal(err)
		}

		listener, err := net.Listen("unix", sockFile)
		logAsFatal(err)

		defer func() {
			_ = listener.Close()
		}()

		server.Addr = address
		logAsFatal(server.Serve(listener))

		return
	}

	server.Addr = fmt.Sprintf("%s:%s", AppHTTPAddress(), strconv.Itoa(AppHTTPPort()))

	// HTTPS
	if IsSSLEnabled() {
		log.Infof("Listening and serving HTTPS on %v", server.Addr)
		logAsFatal(server.ListenAndServeTLS(appSSLCert, appSSLKey))
		return
	}

	// HTTP
	log.Infof("Listening and serving HTTP on %v", server.Addr)
	logAsFatal(server.ListenAndServe())
}

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// Unexported methods
//___________________________________

func aahRecover() {
	if r := recover(); r != nil {
		strace := aruntime.NewStacktrace(r, AppConfig())
		buf := &bytes.Buffer{}
		strace.Print(buf)

		log.Error("Recovered from panic:")
		log.Error(buf.String())
	}
}

func appDir() string {
	if appIsPackaged {
		return AppBaseDir()
	}
	return filepath.Join(AppBaseDir(), "app")
}

func appLogsDir() string {
	return filepath.Join(AppBaseDir(), "logs")
}

func logAsFatal(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func initInternal() {
	logAsFatal(initAppVariables())

	if appBuildInfo == nil {
		// aah CLI is accessing app codebase
		log.SetLevel(log.LevelWarn)
		logAsFatal(initRoutes(appConfigDir(), AppConfig()))
		log.SetLevel(log.LevelDebug)
	} else {
		// publish `OnInit` event
		AppEventStore().sortAndPublishSync(&Event{Name: EventOnInit})

		logAsFatal(initLogs(AppConfig()))
		logAsFatal(initI18n(appI18nDir()))
		logAsFatal(initRoutes(appConfigDir(), AppConfig()))

		if AppMode() == appModeWeb {
			logAsFatal(initTemplateEngine(appViewsDir(), AppConfig()))
		}

		if AppProfile() != appProfileProd {
			logAsFatal(initTests())
		}
	}

	appInitialized = true
}

func initPath(importPath string) error {
	var err error
	goPath, err = ess.GoPath()
	if err != nil {
		return err
	}

	appImportPath = path.Clean(importPath)
	goSrcDir = filepath.Join(goPath, "src")
	appBaseDir = filepath.Join(goSrcDir, filepath.FromSlash(appImportPath))

	appIsPackaged = !ess.IsFileExists(appDir())

	if !appIsPackaged && !ess.IsFileExists(appBaseDir) {
		return fmt.Errorf("aah application does not exists: %s", appImportPath)
	}

	return nil
}

func initAppVariables() error {
	var err error
	cfg := AppConfig()

	appName = cfg.StringDefault("name", filepath.Base(appBaseDir))

	appProfile = cfg.StringDefault("env.active", appDefaultProfile)
	logAsFatal(SetAppProfile(AppProfile()))

	readTimeout := cfg.StringDefault("server.timeout.read", "90s")
	writeTimeout := cfg.StringDefault("server.timeout.write", "90s")
	if !(strings.HasSuffix(readTimeout, "s") || strings.HasSuffix(readTimeout, "m")) &&
		!(strings.HasSuffix(writeTimeout, "s") || strings.HasSuffix(writeTimeout, "m")) {
		return errors.New("'server.timeout.{read|write}' value is not a valid time unit")
	}

	if appHTTPReadTimeout, err = time.ParseDuration(readTimeout); err != nil {
		return fmt.Errorf("'server.timeout.read': %s", err)
	}

	if appHTTPWriteTimeout, err = time.ParseDuration(writeTimeout); err != nil {
		return fmt.Errorf("'server.timeout.write': %s", err)
	}

	maxHdrBytesStr := cfg.StringDefault("server.max_header_bytes", "1mb")
	if maxHdrBytes, er := ess.StrToBytes(maxHdrBytesStr); er == nil {
		appHTTPMaxHdrBytes = int(maxHdrBytes)
	} else {
		return errors.New("'server.max_header_bytes' value is not a valid size unit")
	}

	appSSLCert = cfg.StringDefault("server.ssl.cert", "")
	appSSLKey = cfg.StringDefault("server.ssl.key", "")
	if IsSSLEnabled() && (ess.IsStrEmpty(appSSLCert) || ess.IsStrEmpty(appSSLKey)) {
		return errors.New("HTTP SSL is enabled, so 'server.ssl.cert' & 'server.ssl.key' value is required")
	}

	multipartMemoryStr := cfg.StringDefault("request.multipart_size", "32mb")
	if appMultipartMaxMemory, err = ess.StrToBytes(multipartMemoryStr); err != nil {
		return errors.New("'request.multipart_size' value is not a valid size unit")
	}

	return nil
}

func initLogs(appCfg *config.Config) error {
	if logCfg, found := appCfg.GetSubConfig("log"); found {
		receiver := logCfg.StringDefault("receiver", "")

		if strings.EqualFold(receiver, "file") {
			file := logCfg.StringDefault("file", "")
			if ess.IsStrEmpty(file) {
				logFileName := strings.Replace(AppName(), " ", "-", -1)
				logCfg.SetString("file", filepath.Join(appLogsDir(), logFileName+".log"))
			} else if !filepath.IsAbs(file) {
				logCfg.SetString("file", filepath.Join(appLogsDir(), file))
			}
		}

		logger, err := log.Newc(logCfg)
		if err != nil {
			return err
		}

		log.SetOutput(logger)
	}

	return nil
}

func writePID(appName, appBaseDir string, cfg *config.Config) {
	appPID = os.Getpid()
	pidfile := cfg.StringDefault("pidfile", appName+".pid")
	if !filepath.IsAbs(pidfile) {
		pidfile = filepath.Join(appBaseDir, pidfile)
	}

	if err := ioutil.WriteFile(pidfile, []byte(strconv.Itoa(appPID)), 0644); err != nil {
		log.Error(err)
	}
}

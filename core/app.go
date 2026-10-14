package core

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"github.com/vrischmann/userdir"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"
)

type App struct {
	ctx          context.Context
	assets       embed.FS
	AppName      string `json:"AppName"`
	Version      string `json:"Version"`
	Description  string `json:"Description"`
	Copyright    string `json:"Copyright"`
	UserDir      string `json:"-"`
	LockFile     string `json:"-"`
	PublicCrt    []byte `json:"-"`
	PrivateKey   []byte `json:"-"`
	IsProxy      bool   `json:"IsProxy"`
	proxyMu      sync.Mutex
	SessionToken string `json:"-"`
	startupError error
	IsReset      bool `json:"-"`
}

var (
	appOnce        *App
	globalConfig   *Config
	globalLogger   *Logger
	resourceOnce   *Resource
	systemOnce     *SystemSetup
	proxyOnce      *Proxy
	httpServerOnce *HttpServer
	ruleOnce       *RuleSet
)

func GetApp(assets embed.FS, wjs string) *App {
	if appOnce == nil {
		matches := regexp.MustCompile(`"productVersion":\s*"([\d.]+)"`).FindStringSubmatch(wjs)
		version := "1.0.1"
		if len(matches) > 0 {
			version = matches[1]
		}

		appOnce = &App{
			assets:      assets,
			AppName:     "res-downloader",
			Version:     version,
			Description: "res-downloader是一款集网络资源嗅探 + 高速下载功能于一体的软件，高颜值、高性能和多样化，提供个人用户下载自己上传到各大平台的网络资源功能！",
			Copyright:   "Copyright © 2023~" + strconv.Itoa(time.Now().Year()),
			IsReset:     false,
		}
		appOnce.UserDir = filepath.Join(userdir.GetConfigHome(), appOnce.AppName)
		if directory := os.Getenv("RESD_CONFIG_DIR"); directory != "" {
			if !filepath.IsAbs(directory) {
				panic("RESD_CONFIG_DIR must be an absolute path")
			}
			appOnce.UserDir = directory
		}
		err := os.MkdirAll(appOnce.UserDir, 0750)
		if err != nil {
			fmt.Println("Mkdir UserDir err: ", err.Error())
		}
		appOnce.LockFile = filepath.Join(appOnce.UserDir, "install.lock")
		initLogger()
		initConfig()
		initSystem()
		if err := appOnce.loadCA(); err != nil {
			appOnce.startupError = fmt.Errorf("initialize CA: %w", err)
			globalLogger.Err(appOnce.startupError)
		}
		appOnce.SessionToken = newSessionToken()
		initRule()
		initResource()
		if appOnce.startupError == nil {
			initProxy()
		}
		initHttpServer()
	}
	return appOnce
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	if a.startupError != nil {
		return
	}
	a.startupError = httpServerOnce.start()
	if a.startupError != nil {
		globalLogger.Esg(a.startupError, "start local service")
		return
	}
	if err := systemOnce.recoverProxy(); err != nil {
		globalLogger.Esg(err, "recover system proxy")
	}

}

func (a *App) OnExit() {
	if err := a.UnsetSystemProxy(); err != nil {
		globalLogger.Esg(err, "restore system proxy")
	}
	resourceOnce.shutdown()
	httpServerOnce.stop()
	globalLogger.Close()
	if appOnce.IsReset {
		err := a.ResetApp()
		fmt.Println("err:", err)
	}
}

func (a *App) installCert() (string, error) {
	out, err := systemOnce.installCert()
	if err != nil {
		globalLogger.Esg(err, out)
		return out, err
	} else {
		if err := systemOnce.removeLegacyCert(); err != nil {
			return "remove old shared certificate", err
		}
		if err := a.lock(); err != nil {
			return "save installation status", err
		}
	}
	return out, nil
}

func (a *App) OpenSystemProxy() error {
	a.proxyMu.Lock()
	defer a.proxyMu.Unlock()
	if a.startupError != nil {
		return a.startupError
	}
	if !a.isInstall() {
		return fmt.Errorf("certificate installation required")
	}
	if a.IsProxy {
		return nil
	}
	err := systemOnce.setProxy()
	if err == nil {
		a.IsProxy = true
		return nil
	}
	return err
}

func (a *App) UnsetSystemProxy() error {
	a.proxyMu.Lock()
	defer a.proxyMu.Unlock()
	if !a.IsProxy {
		return nil
	}
	err := systemOnce.unsetProxy()
	if err == nil {
		a.IsProxy = false
		return nil
	}
	return err
}

func (a *App) isInstall() bool {
	data, err := os.ReadFile(a.LockFile)
	return err == nil && string(data) == a.certFingerprint()
}

func (a *App) lock() error {
	err := atomicWriteFile(a.LockFile, []byte(a.certFingerprint()), 0600)
	if err != nil {
		return err
	}
	return nil
}

func (a *App) ResetApp() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return err
	}

	_ = os.Remove(filepath.Join(appOnce.UserDir, "install.lock"))
	_ = os.Remove(filepath.Join(appOnce.UserDir, "pass.cache"))
	_ = os.Remove(filepath.Join(appOnce.UserDir, "config.json"))
	_ = os.Remove(filepath.Join(appOnce.UserDir, "cert.crt"))

	cmd := exec.Command(exePath)
	return cmd.Start()
}

func (a *App) certFingerprint() string {
	sum := sha256.Sum256(a.PublicCrt)
	return hex.EncodeToString(sum[:])
}
func (a *App) proxyEnabled() bool { a.proxyMu.Lock(); defer a.proxyMu.Unlock(); return a.IsProxy }

func (a *App) ShowWindow() {
	if a.ctx != nil {
		wailsruntime.WindowShow(a.ctx)
		wailsruntime.WindowUnminimise(a.ctx)
	}
}

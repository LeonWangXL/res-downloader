package core

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type Bind struct {
}

func NewBind() *Bind {
	return &Bind{}
}

func (b *Bind) Config() *ResponseData {
	return httpServerOnce.buildResp(1, "ok", configSnapshot())
}

func (b *Bind) AppInfo() *ResponseData {
	return httpServerOnce.buildResp(1, "ok", respData{"AppName": appOnce.AppName, "Version": appOnce.Version, "Description": appOnce.Description, "Copyright": appOnce.Copyright, "IsProxy": appOnce.proxyEnabled(), "SessionToken": appOnce.SessionToken, "Port": httpServerOnce.Port, "Host": httpServerOnce.Host, "StartupError": startupErrorMessage()})
}

func (b *Bind) ResetApp() {
	appOnce.IsReset = true
	runtime.Quit(appOnce.ctx)
}

func startupErrorMessage() string {
	if appOnce.startupError != nil {
		return appOnce.startupError.Error()
	}
	return ""
}

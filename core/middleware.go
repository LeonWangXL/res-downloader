package core

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if HandleApi(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func HandleApi(w http.ResponseWriter, r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/api") {
		origin := r.Header.Get("Origin")
		if origin != "" && !localOrigin(origin) {
			http.Error(w, "origin forbidden", 403)
			return true
		}
		if r.Method == http.MethodOptions {
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Resd-Token, Range")
			w.WriteHeader(http.StatusNoContent)
			return true
		}
		token := r.Header.Get("X-Resd-Token")
		if r.URL.Path == "/api/preview" || r.URL.Path == "/api/cert" {
			if token == "" {
				token = r.URL.Query().Get("token")
			}
		}
		if appOnce == nil || appOnce.SessionToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(appOnce.SessionToken)) != 1 {
			http.Error(w, "unauthorized", 401)
			return true
		}
		if remote, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && !net.ParseIP(remote).IsLoopback() {
			http.Error(w, "local access only", 403)
			return true
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Expose-Headers", "Content-Range, Accept-Ranges, Content-Length")
			w.Header().Set("Vary", "Origin")
		}
		expected := http.MethodPost
		switch r.URL.Path {
		case "/api/preview", "/api/cert":
			expected = http.MethodGet
		}
		if r.Method != expected {
			w.Header().Set("Allow", expected)
			http.Error(w, "method not allowed", 405)
			return true
		}
		r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
		switch r.URL.Path {
		case "/api/install":
			httpServerOnce.install(w, r)
		case "/api/set-system-password":
			httpServerOnce.setSystemPassword(w, r)
		case "/api/preview":
			httpServerOnce.preview(w, r)
		case "/api/preview-prepare":
			httpServerOnce.preparePreview(w, r)
		case "/api/proxy-open":
			httpServerOnce.openSystemProxy(w, r)
		case "/api/proxy-unset":
			httpServerOnce.unsetSystemProxy(w, r)
		case "/api/open-directory":
			httpServerOnce.openDirectoryDialog(w, r)
		case "/api/open-file":
			httpServerOnce.openFileDialog(w, r)
		case "/api/open-folder":
			httpServerOnce.openFolder(w, r)
		case "/api/is-proxy":
			httpServerOnce.isProxy(w, r)
		case "/api/app-info":
			httpServerOnce.appInfo(w, r)
		case "/api/set-config":
			httpServerOnce.setConfig(w, r)
		case "/api/get-config":
			httpServerOnce.getConfig(w, r)
		case "/api/set-type":
			httpServerOnce.setType(w, r)
		case "/api/clear":
			httpServerOnce.clear(w, r)
		case "/api/delete":
			httpServerOnce.delete(w, r)
		case "/api/download":
			httpServerOnce.download(w, r)
		case "/api/cancel":
			httpServerOnce.cancel(w, r)
		case "/api/wx-file-decode":
			httpServerOnce.wxFileDecode(w, r)
		case "/api/batch-export":
			httpServerOnce.batchExport(w, r)
		case "/api/tasks":
			httpServerOnce.tasks(w, r)
		case "/api/cert":
			httpServerOnce.downCert(w, r)
		default:
			http.NotFound(w, r)
		}
		return true
	}
	return false
}

func localOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Hostname() == "wails.localhost" {
		return u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "wails"
	}
	return (u.Scheme == "http" || u.Scheme == "https") && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback())
}

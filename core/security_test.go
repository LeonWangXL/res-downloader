package core

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCAIndependentAndPersistent(t *testing.T) {
	a := &App{UserDir: t.TempDir()}
	b := &App{UserDir: t.TempDir()}
	if err := a.loadCA(); err != nil {
		t.Fatal(err)
	}
	if err := b.loadCA(); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.PrivateKey, b.PrivateKey) {
		t.Fatal("shared CA key")
	}
	if _, err := tls.X509KeyPair(a.PublicCrt, a.PrivateKey); err != nil {
		t.Fatal(err)
	}
	again := &App{UserDir: a.UserDir}
	if err := again.loadCA(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.PrivateKey, again.PrivateKey) {
		t.Fatal("CA changed on restart")
	}
}
func TestPasswordCacheIsNotPersisted(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "pass.cache")
	if err := os.WriteFile(cache, []byte("legacy encrypted password"), 0600); err != nil {
		t.Fatal(err)
	}
	system := &SystemSetup{CacheFile: cache}
	system.SetPassword("temporary password", true)
	if system.password() != "temporary password" {
		t.Fatal("session password missing")
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatal("password cache remains")
	}
}

func TestAPIAuthorization(t *testing.T) {
	oldApp, oldHTTP := appOnce, httpServerOnce
	defer func() { appOnce, httpServerOnce = oldApp, oldHTTP }()
	appOnce = &App{SessionToken: "test-token", PublicCrt: []byte("certificate")}
	httpServerOnce = &HttpServer{}
	cases := []struct {
		name, path, method, token, origin, remote string
		status                                    int
	}{
		{"missing token", "/api/cert", "GET", "", "", "127.0.0.1:1234", 401},
		{"preview preparation requires token", "/api/preview-prepare", "POST", "", "http://wails.localhost", "127.0.0.1:1234", 401},
		{"wrong token", "/api/cert", "GET", "wrong", "", "127.0.0.1:1234", 401},
		{"remote origin", "/api/cert", "GET", "test-token", "https://evil.example", "127.0.0.1:1234", 403},
		{"remote address", "/api/cert", "GET", "test-token", "", "192.168.1.2:1234", 403},
		{"wrong method", "/api/cert", "POST", "test-token", "", "127.0.0.1:1234", 405},
		{"valid desktop", "/api/cert", "GET", "test-token", "http://wails.localhost", "127.0.0.1:1234", 200},
		{"query token", "/api/cert?token=test-token", "GET", "", "", "127.0.0.1:1234", 200},
		{"unknown path", "/api/missing", "POST", "test-token", "", "127.0.0.1:1234", 404},
		{"preflight", "/api/download", "OPTIONS", "", "http://wails.localhost", "127.0.0.1:1234", 204},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.RemoteAddr = tc.remote
			req.Header.Set("X-Resd-Token", tc.token)
			req.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()
			if !HandleApi(rec, req) {
				t.Fatal("API not handled")
			}
			if rec.Code != tc.status {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}
func TestConfigValidationAndAtomicSave(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: "8899", TaskNumber: 4, DownNumber: 2, Rule: "*", MimeMap: map[string]MimeInfo{"video/mp4": {Type: "video", Suffix: ".mp4"}}}
	if err := validateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	bad := cfg
	bad.Quality = 5
	if validateConfig(bad) == nil {
		t.Fatal("invalid quality accepted")
	}
	bad = cfg
	bad.Port = "1234junk"
	if validateConfig(bad) == nil {
		t.Fatal("invalid port accepted")
	}
	file := filepath.Join(t.TempDir(), "config.json")
	if err := atomicWriteFile(file, []byte(`{"value":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(file, []byte(`{"value":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	var saved map[string]int
	if err := json.Unmarshal(data, &saved); err != nil || saved["value"] != 2 {
		t.Fatal(string(data), err)
	}
}

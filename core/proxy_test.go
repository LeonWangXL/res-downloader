package core

import (
	"crypto/tls"
	"crypto/x509"
	"github.com/elazarl/goproxy"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"res-downloader/core/shared"
	"runtime"
	"testing"
)

func TestProxyCapturesHTTPAndHTTPS(t *testing.T) {
	oldApp, oldConfig, oldProxy, oldResource, oldHTTP, oldRule := appOnce, globalConfig, proxyOnce, resourceOnce, httpServerOnce, ruleOnce
	defer func() {
		appOnce, globalConfig, proxyOnce, resourceOnce, httpServerOnce, ruleOnce = oldApp, oldConfig, oldProxy, oldResource, oldHTTP, oldRule
	}()
	cert, key, err := generateCA()
	if err != nil {
		t.Fatal(err)
	}
	appOnce = &App{PublicCrt: cert, PrivateKey: key, Version: "test"}
	globalConfig = &Config{MimeMap: getDefaultMimeMap(), UseHeaders: "default", Rule: "*"}
	resourceOnce = nil
	initResource()
	httpServerOnce = &HttpServer{}
	ruleOnce = &RuleSet{}
	ruleOnce.Load("*")
	proxyOnce = nil
	initProxy()
	origin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		io.WriteString(w, "media")
	})
	plain := httptest.NewServer(origin)
	defer plain.Close()
	secure := httptest.NewTLSServer(origin)
	defer secure.Close()
	originRoots := x509.NewCertPool()
	originRoots.AddCert(secure.Certificate())
	proxyOnce.Proxy.Tr.TLSClientConfig = &tls.Config{RootCAs: originRoots, MinVersion: tls.VersionTLS12}
	sources := make(chan shared.ApplicationInfo, 2)
	proxyOnce.Proxy.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		if resp != nil {
			sources <- shared.RequestApplication(resp.Request)
		}
		return resp
	})
	proxy := httptest.NewUnstartedServer(proxyOnce.Proxy)
	proxy.Config.ConnContext = sourceConnectionContext
	proxy.Start()
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(cert)
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	for _, rawURL := range []string{plain.URL + "/media.mp4", secure.URL + "/media.mp4"} {
		resp, err := client.Get(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || string(body) != "media" {
			t.Fatal(string(body), err)
		}
		if !resourceOnce.mediaIsMarked(shared.Md5(rawURL)) {
			t.Fatal("proxy did not capture resource", rawURL)
		}
		source := <-sources
		if runtime.GOOS == "windows" && (source.PID != uint32(os.Getpid()) || source.Name == "") {
			t.Fatalf("proxy lost application attribution for %s: %+v", rawURL, source)
		}
	}
}

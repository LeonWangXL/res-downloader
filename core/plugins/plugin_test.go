package plugins

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"res-downloader/core/shared"
	"strings"
	"testing"
)

func TestWechatScriptCaptureWithExplicitHTTPSPort(t *testing.T) {
	p := &QqPlugin{}
	p.SetBridge(&shared.Bridge{GetVersion: func() string { return "test" }, TypeSuffix: func(string) (string, string) { return "", "" }})
	req := httptest.NewRequest("GET", "https://channels.weixin.qq.com:443/web/pages/feed", nil)
	resp := &http.Response{StatusCode: 200, Request: req, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`<script src="app.js"></script>`))}
	result := p.OnResponse(resp, nil)
	if result == nil {
		t.Fatal("explicit port disabled WeChat script injection")
	}
	body, err := io.ReadAll(result.Body)
	if err != nil || !strings.Contains(string(body), `.js?v=test"`) {
		t.Fatal(string(body), err)
	}
}

func TestReadRewriteBodyGzipAndPreserveUnsupported(t *testing.T) {
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	w.Write([]byte("get media(){return 1}"))
	w.Close()
	resp := &http.Response{Header: http.Header{"Content-Encoding": {"gzip"}}, Body: io.NopCloser(bytes.NewReader(compressed.Bytes()))}
	body, err := readRewriteBody(resp)
	if err != nil || !strings.Contains(string(body), "get media()") {
		t.Fatal(string(body), err)
	}
	if resp.Header.Get("Content-Encoding") != "" {
		t.Fatal("gzip header remains after decoding")
	}
	resp = &http.Response{Header: http.Header{"Content-Encoding": {"br"}}, Body: io.NopCloser(strings.NewReader("opaque"))}
	if _, err := readRewriteBody(resp); err == nil {
		t.Fatal("unsupported encoding accepted")
	}
	body, _ = io.ReadAll(resp.Body)
	if string(body) != "opaque" {
		t.Fatal("original response lost")
	}
}

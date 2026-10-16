package plugins

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"res-downloader/core/shared"
	"strings"
	"testing"
	"time"
)

func TestMediaPluginsPreserveConnectionApplication(t *testing.T) {
	source := shared.ApplicationInfo{Name: "WeChat.exe", PID: 1234}
	for _, kind := range []string{"default", "wechat"} {
		t.Run(kind, func(t *testing.T) {
			captured := make(chan shared.MediaInfo, 1)
			bridge := &shared.Bridge{
				TypeSuffix:    func(string) (string, string) { return "video", ".mp4" },
				GetResType:    func(string) (bool, bool) { return true, true },
				MediaIsMarked: func(string) bool { return false }, MarkMedia: func(string) bool { return true },
				Send: func(_ string, data interface{}) { captured <- data.(shared.MediaInfo) },
			}
			if kind == "default" {
				plugin := &DefaultPlugin{}
				plugin.SetBridge(bridge)
				req := httptest.NewRequest("GET", "https://video.example/file.mp4", nil)
				req = req.WithContext(shared.WithApplication(context.Background(), source))
				plugin.OnResponse(&http.Response{StatusCode: 200, Request: req, Header: http.Header{"Content-Type": {"video/mp4"}}}, nil)
			} else {
				plugin := &QqPlugin{}
				plugin.SetBridge(bridge)
				plugin.handleMedia([]byte(`{"media":[{"url":"https://video.example/file.mp4"}]}`), source)
			}
			select {
			case media := <-captured:
				if media.SourceApp != source.Name || media.SourcePID != source.PID {
					t.Fatalf("source application lost: %+v", media)
				}
			case <-time.After(time.Second):
				t.Fatal("resource was not emitted")
			}
		})
	}
}

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

func TestWechatDeduplicatesCompleteSignedResourceAddress(t *testing.T) {
	marked := map[string]bool{}
	captured := make(chan shared.MediaInfo, 3)
	bridge := &shared.Bridge{
		GetResType:    func(string) (bool, bool) { return true, true },
		MediaIsMarked: func(key string) bool { return marked[key] },
		MarkMedia: func(key string) bool {
			if marked[key] {
				return false
			}
			marked[key] = true
			return true
		},
		TypeSuffix: func(string) (string, string) { return "video", ".mp4" },
		Send:       func(_ string, data interface{}) { captured <- data.(shared.MediaInfo) },
	}
	plugin := &QqPlugin{}
	plugin.SetBridge(bridge)
	first := []byte(`{"media":[{"url":"https://video.example/file.mp4?key=original","urlToken":"&token=first"}]}`)
	second := []byte(`{"media":[{"url":"https://video.example/file.mp4?key=original","urlToken":"&token=second"}]}`)
	plugin.handleMedia(first, shared.ApplicationInfo{})
	plugin.handleMedia(first, shared.ApplicationInfo{})
	plugin.handleMedia(second, shared.ApplicationInfo{})
	normal := &DefaultPlugin{}
	normal.SetBridge(bridge)
	req := httptest.NewRequest("GET", "https://video.example/file.mp4?key=original&token=first", nil)
	normal.OnResponse(&http.Response{StatusCode: 200, Request: req, Header: http.Header{"Content-Type": {"video/mp4"}}}, nil)
	if len(marked) != 2 {
		t.Fatalf("expected two distinct complete URLs, got %d", len(marked))
	}
	for i := 0; i < 2; i++ {
		select {
		case media := <-captured:
			if media.UrlSign != shared.Md5(media.Url) {
				t.Fatal("deduplication key differs from final URL", media.Url)
			}
		case <-time.After(time.Second):
			t.Fatal("distinct signed URL was discarded")
		}
	}
}

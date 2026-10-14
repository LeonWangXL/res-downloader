package core

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"res-downloader/core/shared"
	"strings"
	"testing"
	"time"
)

func TestEncryptedPreviewStreamsAndSeeksAtCorrectOffsets(t *testing.T) {
	oldApp, oldHTTP, oldResource, oldConfig := appOnce, httpServerOnce, resourceOnce, globalConfig
	defer func() { appOnce, httpServerOnce, resourceOnce, globalConfig = oldApp, oldHTTP, oldResource, oldConfig }()
	appOnce = &App{SessionToken: "test-token"}
	globalConfig = &Config{UseHeaders: "default"}
	resourceOnce = &Resource{}
	plain := make([]byte, 200000)
	key := make([]byte, 131072)
	for i := range plain {
		plain[i] = byte(i % 239)
	}
	for i := range key {
		key[i] = byte(i%251 + 1)
	}
	encrypted := bytes.Clone(plain)
	for i := range key {
		encrypted[i] ^= key[i]
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "video.mp4", time.Time{}, bytes.NewReader(encrypted))
	}))
	defer origin.Close()
	httpServerOnce = &HttpServer{previewClient: origin.Client()}
	raw := origin.URL + "/video.mp4"
	payload, _ := json.Marshal(map[string]interface{}{"Url": raw, "Id": "encrypted", "UrlSign": "encrypted-sign", "decodeStr": base64.StdEncoding.EncodeToString(key)})
	prepare := httptest.NewRequest("POST", "/api/preview-prepare", bytes.NewReader(payload))
	prepare.RemoteAddr = "127.0.0.1:1234"
	prepare.Header.Set("X-Resd-Token", "test-token")
	prepared := httptest.NewRecorder()
	HandleApi(prepared, prepare)
	if !strings.Contains(prepared.Body.String(), `"code":1`) {
		t.Fatal(prepared.Body.String())
	}
	for _, item := range []struct {
		header     string
		start, end int
	}{{"", 0, len(plain)}, {"bytes=0-32767", 0, 32768}, {"bytes=130000-132000", 130000, 132001}, {"bytes=150000-", 150000, len(plain)}} {
		req := httptest.NewRequest("GET", "/api/preview?token=test-token&url="+url.QueryEscape(raw), nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Range", item.header)
		rec := httptest.NewRecorder()
		HandleApi(rec, req)
		if rec.Code != 200 && rec.Code != 206 {
			t.Fatal(rec.Code, rec.Body.String())
		}
		if !bytes.Equal(rec.Body.Bytes(), plain[item.start:item.end]) {
			t.Fatalf("bad decrypted bytes at %s", item.header)
		}
		if rec.Header().Get("Content-Type") != "video/mp4" || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("bad decoded media headers", rec.Header())
		}
	}
	resourceOnce.delete("encrypted-sign")
	if _, ok := resourceOnce.previewKeys.Load(raw); ok {
		t.Fatal("deleted preview key retained")
	}
}

func TestPreviewRangeAndDesktopCORS(t *testing.T) {
	oldApp, oldHTTP, oldResource, oldConfig := appOnce, httpServerOnce, resourceOnce, globalConfig
	defer func() { appOnce, httpServerOnce, resourceOnce, globalConfig = oldApp, oldHTTP, oldResource, oldConfig }()
	appOnce = &App{SessionToken: "test-token"}
	globalConfig = &Config{UseHeaders: "default", UserAgent: "preview-agent"}
	resourceOnce = &Resource{}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=0-4" || r.Header.Get("Cookie") != "session=secret" || r.Header.Get("Referer") != "https://source.example/" {
			t.Errorf("preview lost request headers: %v", r.Header)
		}
		w.Header().Set("Content-Range", "bytes 0-4/10")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusPartialContent)
		io.WriteString(w, "media")
	}))
	defer origin.Close()
	raw := origin.URL + "/video.mp4?token=a%2Bb"
	resourceOnce.previewMedia.Store(raw, shared.MediaInfo{OtherData: map[string]string{"headers": `{"Cookie":["session=secret"],"Referer":["https://source.example/"],"Range":["bytes=99-100"]}`}})
	httpServerOnce = &HttpServer{previewClient: origin.Client()}
	req := httptest.NewRequest("GET", "/api/preview?token=test-token&url="+url.QueryEscape(raw), nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Origin", "http://wails.localhost")
	req.Header.Set("Range", "bytes=0-4")
	rec := httptest.NewRecorder()
	HandleApi(rec, req)
	if rec.Code != 206 || rec.Body.String() != "media" || rec.Header().Get("Content-Range") != "bytes 0-4/10" {
		t.Fatalf("preview response %d %s %v", rec.Code, rec.Body.String(), rec.Header())
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Expose-Headers"), "Content-Range") {
		t.Fatal("desktop cannot read range metadata")
	}
	req = httptest.NewRequest("OPTIONS", "/api/preview", nil)
	req.Header.Set("Origin", "http://wails.localhost")
	req.Header.Set("Access-Control-Request-Headers", "range")
	rec = httptest.NewRecorder()
	HandleApi(rec, req)
	if rec.Code != 204 || !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Range") {
		t.Fatal("range preflight blocked")
	}
}

func TestPreviewPlaylistRoutesSegmentsAndKeysLocally(t *testing.T) {
	oldApp, oldResource := appOnce, resourceOnce
	defer func() { appOnce, resourceOnce = oldApp, oldResource }()
	appOnce = &App{SessionToken: "local-secret"}
	resourceOnce = &Resource{}
	source := "https://cdn.example/path/list.m3u8?signature=abc"
	resourceOnce.previewMedia.Store(source, shared.MediaInfo{OtherData: map[string]string{"headers": `{"Referer":["https://source.example/"]}`}})
	base, _ := url.Parse(source)
	server := &HttpServer{Host: "127.0.0.1", Port: "8899"}
	body, err := server.rewritePlaylist([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"../key.bin?token=a%2Bb\"\n#EXTINF:3,\nsegment.ts\n"), base, source)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(body), "\n")
	keyURL := strings.Split(lines[1], `URI="`)[1]
	keyURL = strings.TrimSuffix(keyURL, `"`)
	for _, item := range []struct{ local, target string }{{keyURL, "https://cdn.example/key.bin?token=a%2Bb"}, {lines[3], "https://cdn.example/path/segment.ts"}} {
		parsed, err := url.Parse(item.local)
		if err != nil || parsed.Host != "127.0.0.1:8899" || parsed.Query().Get("token") != "local-secret" || parsed.Query().Get("url") != item.target {
			t.Fatalf("bad playlist URL %s", item.local)
		}
		if _, ok := resourceOnce.previewMedia.Load(item.target); !ok {
			t.Fatal("segment lost captured headers")
		}
	}
	if _, err := server.rewritePlaylist([]byte("#EXTM3U\nfile:///secret\n"), base, source); err == nil {
		t.Fatal("non-HTTP playlist URI accepted")
	}
}

func TestPreviewProbeReportsFailuresAndPreservesSignedURL(t *testing.T) {
	oldApp, oldHTTP, oldResource, oldConfig := appOnce, httpServerOnce, resourceOnce, globalConfig
	defer func() { appOnce, httpServerOnce, resourceOnce, globalConfig = oldApp, oldHTTP, oldResource, oldConfig }()
	appOnce = &App{SessionToken: "test-token"}
	globalConfig = &Config{UseHeaders: "default"}
	resourceOnce = &Resource{}
	key := bytes.Repeat([]byte{37}, 131072)
	plain := make([]byte, 512)
	copy(plain[4:], []byte("ftypisom"))
	encrypted := bytes.Clone(plain)
	for i := range encrypted {
		encrypted[i] ^= key[i]
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.RawQuery, "token=a%2Bb&signature=keep+exact") {
			t.Errorf("signed URL rewritten: %s", r.URL.RawQuery)
		}
		if r.Header.Get("Range") != "bytes=0-4095" {
			t.Errorf("probe range missing: %s", r.Header.Get("Range"))
		}
		switch r.URL.Query().Get("mode") {
		case "denied":
			w.WriteHeader(403)
		case "html":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, "<html>login</html>")
		case "encrypted":
			w.Write(encrypted)
		case "invalid":
			w.Write([]byte("invalid encrypted media"))
		default:
			w.Write(plain)
		}
	}))
	defer origin.Close()
	httpServerOnce = &HttpServer{previewClient: origin.Client()}
	check := func(mode, decodeStr string) ResponseData {
		raw := origin.URL + "/media.mp4?mode=" + mode + "&token=a%2Bb&signature=keep+exact"
		body, _ := json.Marshal(map[string]interface{}{"Url": raw, "Classify": "video", "check": true, "decodeStr": decodeStr})
		req := httptest.NewRequest("POST", "/api/preview-prepare", bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Resd-Token", "test-token")
		rec := httptest.NewRecorder()
		HandleApi(rec, req)
		var result ResponseData
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err, rec.Body.String())
		}
		return result
	}
	for _, tc := range []struct {
		mode, reason string
		code         int
	}{{"denied", "http", 0}, {"html", "non_media", 0}, {"invalid", "decrypt", 0}, {"encrypted", "", 1}, {"plain", "", 1}} {
		t.Run(tc.mode, func(t *testing.T) {
			result := check(tc.mode, base64.StdEncoding.EncodeToString(key))
			if result.Code != tc.code {
				t.Fatalf("unexpected probe result: %+v", result)
			}
			if tc.reason != "" && result.Data.(map[string]interface{})["reason"] != tc.reason {
				t.Fatal(result.Data)
			}
			if tc.mode == "plain" {
				if _, ok := resourceOnce.previewKeys.Load(origin.URL + "/media.mp4?mode=plain&token=a%2Bb&signature=keep+exact"); ok {
					t.Fatal("plaintext media would be decrypted twice")
				}
			}
		})
	}
	origin.Close()
	result := check("offline", "")
	if result.Code != 0 || result.Data.(map[string]interface{})["reason"] != "network" {
		t.Fatal(result)
	}
}

package core

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"res-downloader/core/shared"
	"sync/atomic"
	"testing"
	"time"
)

func TestResourceQueueLimitDuplicateCancelAndCleanup(t *testing.T) {
	oldConfig, oldApp, oldHTTP := globalConfig, appOnce, httpServerOnce
	defer func() { globalConfig, appOnce, httpServerOnce = oldConfig, oldApp, oldHTTP }()
	globalConfig = &Config{SaveDirectory: t.TempDir(), TaskNumber: 1, DownNumber: 1, UseHeaders: "default"}
	appOnce = &App{}
	httpServerOnce = &HttpServer{}
	var gets, max, active atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4")
		if r.Method == "HEAD" {
			return
		}
		gets.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := max.Load(); n > old; old = max.Load() {
			if max.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-release:
			fmt.Fprint(w, "data")
		case <-r.Context().Done():
		}
	}))
	defer s.Close()
	r := &Resource{jobs: make(map[string]*resourceJob)}
	defer r.shutdown()
	info := shared.MediaInfo{Id: "one", Url: s.URL + "/one", Suffix: ".bin"}
	if err := r.download(info, ""); err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := r.download(info, ""); err == nil {
		t.Fatal("duplicate job accepted")
	}
	two := info
	two.Id = "two"
	two.Url = s.URL + "/two"
	if err := r.download(two, ""); err != nil {
		t.Fatal(err)
	}
	if gets.Load() != 1 {
		t.Fatal("queue limit ignored")
	}
	if len(r.taskSnapshot()) != 2 {
		t.Fatal("missing queued task")
	}
	if err := r.cancel("two"); err != nil {
		t.Fatal(err)
	}
	if len(r.taskSnapshot()) != 1 {
		t.Fatal("cancelled task retained")
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for len(r.taskSnapshot()) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(r.taskSnapshot()) != 0 || max.Load() != 1 {
		t.Fatal("jobs not cleaned up or concurrency exceeded")
	}
	if err := r.cancel("one"); err == nil {
		t.Fatal("completed task can still be cancelled")
	}
	matches, _ := filepath.Glob(filepath.Join(globalConfig.SaveDirectory, "*.bin"))
	if len(matches) != 1 {
		t.Fatal(matches)
	}
	data, _ := os.ReadFile(matches[0])
	if string(data) != "data" {
		t.Fatal(string(data))
	}
}
func TestConfigPortDoesNotChangeLiveListener(t *testing.T) {
	oldConfig, oldHTTP, oldRule, oldResource := globalConfig, httpServerOnce, ruleOnce, resourceOnce
	defer func() {
		globalConfig, httpServerOnce, ruleOnce, resourceOnce = oldConfig, oldHTTP, oldRule, oldResource
	}()
	cfg := &Config{Host: "127.0.0.1", Port: "8899", TaskNumber: 4, DownNumber: 3, Rule: "*", MimeMap: map[string]MimeInfo{}}
	cfg.storage = &Storage{fileName: filepath.Join(t.TempDir(), "config.json")}
	globalConfig = cfg
	httpServerOnce = &HttpServer{Port: "8899"}
	ruleOnce = &RuleSet{}
	resourceOnce = nil
	next := *cfg
	next.Port = "8900"
	if err := cfg.setConfig(next); err != nil {
		t.Fatal(err)
	}
	if httpServerOnce.Port != "8899" || configSnapshot().Port != "8900" {
		t.Fatal("saved and live port mixed")
	}
}
func TestDecodeKeepsSourceAndRejectsShortFile(t *testing.T) {
	r := &Resource{}
	source := filepath.Join(t.TempDir(), "source.MP4")
	if err := os.WriteFile(source, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.wxFileDecode(shared.MediaInfo{}, source, "AAAAAAAAAA=="); err == nil {
		t.Fatal("short file decoded")
	}
	got, _ := os.ReadFile(source)
	if string(got) != "abc" {
		t.Fatal("source file overwritten")
	}
}

package core

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func readDownloaded(t *testing.T, fd *FileDownloader) []byte {
	t.Helper()
	b, err := os.ReadFile(fd.FileName)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestDownloaderShortBodyFails(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", "10")
			return
		}
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "abc")
	}))
	defer s.Close()
	fd := NewFileDownloader(s.URL, filepath.Join(t.TempDir(), "x"), 1, nil)
	if err := fd.Start(); err == nil {
		t.Fatal("short response accepted")
	}
	if _, err := os.Stat(fd.FileName); !os.IsNotExist(err) {
		t.Fatalf("failed download remains: %v", err)
	}
}
func TestDownloaderSingleRetryRestarts(t *testing.T) {
	var gets atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", "10")
			return
		}
		if gets.Add(1) == 1 {
			c, b, _ := w.(http.Hijacker).Hijack()
			b.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\nabc")
			b.Flush()
			c.Close()
			return
		}
		fmt.Fprint(w, "abcdefghij")
	}))
	defer s.Close()
	fd := NewFileDownloader(s.URL, filepath.Join(t.TempDir(), "x"), 1, nil)
	if err := fd.Start(); err != nil {
		t.Fatal(err)
	}
	if got := string(readDownloaded(t, fd)); got != "abcdefghij" {
		t.Fatalf("corrupt retry: %q", got)
	}
}
func TestDownloaderHeadUnsupported(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", "3")
			w.WriteHeader(405)
			return
		}
		fmt.Fprint(w, "abcdefghij")
	}))
	defer s.Close()
	fd := NewFileDownloader(s.URL, filepath.Join(t.TempDir(), "x"), 1, nil)
	if err := fd.Start(); err != nil {
		t.Fatal(err)
	}
	if fd.TotalSize != 10 || string(readDownloaded(t, fd)) != "abcdefghij" {
		t.Fatal("HEAD error used as metadata")
	}
}
func TestDownloaderMultipartAndFallback(t *testing.T) {
	data := bytes.Repeat([]byte("abcdefgh"), 400000)
	for _, badRange := range []bool{false, true} {
		t.Run(strconv.FormatBool(badRange), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Accept-Ranges", "bytes")
				if r.Method == "HEAD" {
					w.Header().Set("Content-Length", strconv.Itoa(len(data)))
					return
				}
				if h := r.Header.Get("Range"); h != "" {
					var a, b int
					fmt.Sscanf(h, "bytes=%d-%d", &a, &b)
					if badRange {
						w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", b, len(data)))
					} else {
						w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, b, len(data)))
					}
					w.WriteHeader(206)
					w.Write(data[a : b+1])
					return
				}
				w.Write(data)
			}))
			defer s.Close()
			fd := NewFileDownloader(s.URL, filepath.Join(t.TempDir(), "x"), 3, nil)
			if err := fd.Start(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(readDownloaded(t, fd), data) {
				t.Fatal("multipart content mismatch")
			}
		})
	}
}
func TestDownloaderCancellationDuringProbe(t *testing.T) {
	entered := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer s.Close()
	fd := NewFileDownloader(s.URL, filepath.Join(t.TempDir(), "x"), 1, nil)
	done := make(chan error, 1)
	go func() { done <- fd.Start() }()
	<-entered
	fd.Cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled probe succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("probe ignored cancellation")
	}
}
func TestDownloaderConcurrentNames(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4")
		if r.Method != "HEAD" {
			fmt.Fprint(w, "data")
		}
	}))
	defer s.Close()
	dir := t.TempDir()
	var wg sync.WaitGroup
	names := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fd := NewFileDownloader(s.URL, filepath.Join(dir, "x.bin"), 1, nil)
			if err := fd.Start(); err != nil {
				t.Error(err)
				return
			}
			names <- fd.FileName
		}()
	}
	wg.Wait()
	close(names)
	seen := map[string]bool{}
	for name := range names {
		if seen[name] {
			t.Fatal("shared output filename")
		}
		seen[name] = true
	}
	if len(seen) != 8 {
		t.Fatal("missing outputs")
	}
}
func TestDownloaderHeaderSelection(t *testing.T) {
	fd := NewFileDownloader("http://example.com", "", 1, map[string]string{"Cookie": "secret", "Referer": "source", "Range": "bytes=100-", "User-Agent": "agent"})
	fd.config.UseHeaders = "referer,user-agent"
	req, _ := http.NewRequestWithContext(context.Background(), "GET", fd.Url, nil)
	fd.setHeaders(req)
	if req.Header.Get("Cookie") != "" || req.Header.Get("Range") != "" || req.Header.Get("Referer") != "source" || !strings.Contains(req.Header.Get("User-Agent"), "agent") {
		t.Fatal(req.Header)
	}
}

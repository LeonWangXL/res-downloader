package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	MaxRetries  = 3
	RetryDelay  = 3 * time.Second
	MinPartSize = 1024 * 1024
)

type ProgressCallback func(totalDownloaded, totalSize float64, taskID int, taskProgress float64)
type DownloadTask struct {
	taskID                               int
	rangeStart, rangeEnd, downloadedSize int64
}
type FileDownloader struct {
	Url, Referer, FileName string
	ProxyUrl               *url.URL
	File                   *os.File
	TotalSize              int64
	IsMultiPart            bool
	totalTasks             int
	Headers                map[string]string
	DownloadTaskList       []*DownloadTask
	progressCallback       ProgressCallback
	ctx                    context.Context
	cancelFunc             context.CancelFunc
	config                 Config
	client                 *http.Client
	tempName               string
	validator              string
	downloaded             atomic.Int64
	total                  atomic.Int64
}

func NewFileDownloader(rawURL, filename string, totalTasks int, headers map[string]string) *FileDownloader {
	ctx, cancel := context.WithCancel(context.Background())
	copyHeaders := make(map[string]string, len(headers))
	for k, v := range headers {
		copyHeaders[k] = v
	}
	fd := &FileDownloader{Url: rawURL, FileName: filename, totalTasks: totalTasks, Headers: copyHeaders, ctx: ctx, cancelFunc: cancel, config: configSnapshot(), TotalSize: -1}
	fd.total.Store(-1)
	return fd
}
func (fd *FileDownloader) buildClient() *http.Client {
	tr := &http.Transport{DialContext: (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext, MaxIdleConnsPerHost: 16, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second, DisableCompression: true}
	if fd.ProxyUrl != nil {
		tr.Proxy = http.ProxyURL(fd.ProxyUrl)
	}
	return &http.Client{Transport: tr}
}

var forbiddenDownloadHeaders = map[string]bool{"accept-encoding": true, "content-length": true, "host": true, "connection": true, "keep-alive": true, "proxy-connection": true, "transfer-encoding": true, "range": true, "if-range": true, "if-none-match": true, "if-modified-since": true, "x-forwarded-for": true, "x-real-ip": true}

func (fd *FileDownloader) setHeaders(req *http.Request) {
	allowed := map[string]bool{}
	for _, name := range strings.Split(fd.config.UseHeaders, ",") {
		allowed[strings.ToLower(strings.TrimSpace(name))] = true
	}
	for k, v := range fd.Headers {
		name := strings.ToLower(k)
		if forbiddenDownloadHeaders[name] || strings.HasPrefix(name, "sec-") {
			continue
		}
		if fd.config.UseHeaders == "default" || allowed[name] {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("Accept-Encoding", "identity")
}
func (fd *FileDownloader) init() error {
	u, err := url.Parse(fd.Url)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("invalid download URL")
	}
	fd.Referer = u.Scheme + "://" + u.Host + "/"
	if fd.config.DownloadProxy && fd.config.UpstreamProxy != "" {
		fd.ProxyUrl, err = url.Parse(fd.config.UpstreamProxy)
		if err != nil {
			return err
		}
	}
	if fd.Headers["User-Agent"] == "" {
		fd.Headers["User-Agent"] = fd.config.UserAgent
	}
	if fd.Headers["Referer"] == "" {
		fd.Headers["Referer"] = fd.Referer
	}
	fd.client = fd.buildClient()
	probeCtx, cancel := context.WithTimeout(fd.ctx, 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(probeCtx, "HEAD", fd.Url, nil)
	fd.setHeaders(req)
	resp, err := fd.client.Do(req)
	if err != nil {
		return fmt.Errorf("probe resource: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		fd.TotalSize = resp.ContentLength
		fd.validator = resp.Header.Get("ETag")
		if strings.HasPrefix(fd.validator, "W/") {
			fd.validator = ""
		}
		if fd.validator == "" {
			fd.validator = resp.Header.Get("Last-Modified")
		}
		fd.IsMultiPart = resp.Header.Get("Accept-Ranges") == "bytes" && fd.TotalSize > MinPartSize
	} else if resp.StatusCode == 405 || resp.StatusCode == 501 {
		fd.TotalSize = -1
	} else {
		return fmt.Errorf("probe status: %d", resp.StatusCode)
	}
	fd.total.Store(fd.TotalSize)
	if err := fd.ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fd.FileName), 0750); err != nil {
		return err
	}
	base, ext := strings.TrimSuffix(fd.FileName, filepath.Ext(fd.FileName)), filepath.Ext(fd.FileName)
	for i := 0; ; i++ {
		candidate := fd.FileName
		if i > 0 {
			candidate = fmt.Sprintf("%s(%d)%s", base, i, ext)
		}
		f, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		fd.FileName = candidate
		if err := f.Close(); err != nil {
			os.Remove(candidate)
			return err
		}
		break
	}
	fd.File, err = os.CreateTemp(filepath.Dir(fd.FileName), ".resd-*.part")
	if err != nil {
		os.Remove(fd.FileName)
		return err
	}
	fd.tempName = fd.File.Name()
	return nil
}
func (fd *FileDownloader) createDownloadTasks() {
	fd.DownloadTaskList = nil
	count := 1
	if fd.IsMultiPart {
		count = fd.totalTasks
		if count < 1 {
			count = 4
		}
		if count > 64 {
			count = 64
		}
		if max := int(fd.TotalSize / MinPartSize); count > max {
			count = max
		}
	}
	for i := 0; i < count; i++ {
		start, end := int64(0), fd.TotalSize-1
		if fd.IsMultiPart {
			start = fd.TotalSize * int64(i) / int64(count)
			end = fd.TotalSize*int64(i+1)/int64(count) - 1
		}
		fd.DownloadTaskList = append(fd.DownloadTaskList, &DownloadTask{taskID: i, rangeStart: start, rangeEnd: end})
	}
}
func (fd *FileDownloader) startDownload() error {
	var wg sync.WaitGroup
	errs := make(chan error, len(fd.DownloadTaskList))
	for _, task := range fd.DownloadTaskList {
		wg.Add(1)
		go func(task *DownloadTask) {
			defer wg.Done()
			var err error
			for attempt := 0; attempt < MaxRetries; attempt++ {
				err = fd.doDownloadTask(task)
				if err == nil || fd.ctx.Err() != nil {
					break
				}
				if attempt < MaxRetries-1 {
					select {
					case <-fd.ctx.Done():
						err = fd.ctx.Err()
					case <-time.After(RetryDelay):
					}
				}
			}
			if err != nil {
				errs <- err
			}
		}(task)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	return fd.verifyDownload()
}
func (fd *FileDownloader) doDownloadTask(task *DownloadTask) error {
	if err := fd.ctx.Err(); err != nil {
		return err
	}
	if !fd.IsMultiPart {
		fd.downloaded.Add(-task.downloadedSize)
		task.downloadedSize = 0
		if err := fd.File.Truncate(0); err != nil {
			return err
		}
	}
	if fd.IsMultiPart && task.downloadedSize == task.rangeEnd-task.rangeStart+1 {
		return nil
	}
	req, err := http.NewRequestWithContext(fd.ctx, "GET", fd.Url, nil)
	if err != nil {
		return err
	}
	fd.setHeaders(req)
	start := task.rangeStart + task.downloadedSize
	if fd.IsMultiPart {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, task.rangeEnd))
		if fd.validator != "" {
			req.Header.Set("If-Range", fd.validator)
		}
	}
	resp, err := fd.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if fd.IsMultiPart {
		var a, b, total int64
		if resp.StatusCode != 206 {
			return fmt.Errorf("range status: %d", resp.StatusCode)
		}
		if _, err := fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &a, &b, &total); err != nil || a != start || b != task.rangeEnd || total != fd.TotalSize {
			return errors.New("invalid Content-Range")
		}
	} else {
		if resp.StatusCode != 200 {
			return fmt.Errorf("download status: %d", resp.StatusCode)
		}
		if fd.TotalSize < 0 && resp.ContentLength >= 0 {
			fd.TotalSize = resp.ContentLength
			fd.total.Store(fd.TotalSize)
			task.rangeEnd = fd.TotalSize - 1
		}
	}
	// A stalled body is closed without limiting the duration of a healthy large download.
	bodyCtx, bodyCancel := context.WithCancel(fd.ctx)
	defer bodyCancel()
	var lastRead atomic.Int64
	lastRead.Store(time.Now().UnixNano())
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-bodyCtx.Done():
				return
			case <-ticker.C:
				if time.Since(time.Unix(0, lastRead.Load())) > 30*time.Second {
					resp.Body.Close()
					return
				}
			}
		}
	}()
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			lastRead.Store(time.Now().UnixNano())
			if fd.TotalSize >= 0 && task.downloadedSize+int64(n) > task.rangeEnd-task.rangeStart+1 {
				return errors.New("response exceeds expected size")
			}
			written, err := fd.File.WriteAt(buf[:n], task.rangeStart+task.downloadedSize)
			if err != nil {
				return err
			}
			if written != n {
				return io.ErrShortWrite
			}
			task.downloadedSize += int64(n)
			fd.downloaded.Add(int64(n))
		}
		if readErr != nil {
			if readErr != io.EOF {
				return readErr
			}
			if fd.TotalSize >= 0 && task.downloadedSize != task.rangeEnd-task.rangeStart+1 {
				return io.ErrUnexpectedEOF
			}
			return nil
		}
		if err := fd.ctx.Err(); err != nil {
			return err
		}
	}
}
func (fd *FileDownloader) verifyDownload() error {
	var total int64
	for _, task := range fd.DownloadTaskList {
		if fd.TotalSize >= 0 && task.downloadedSize != task.rangeEnd-task.rangeStart+1 {
			return io.ErrUnexpectedEOF
		}
		total += task.downloadedSize
	}
	info, err := fd.File.Stat()
	if err != nil {
		return err
	}
	if info.Size() != total || (fd.TotalSize >= 0 && total != fd.TotalSize) {
		return errors.New("download size mismatch")
	}
	return nil
}
func (fd *FileDownloader) Start() (err error) {
	defer fd.cancelFunc()
	if err = fd.init(); err != nil {
		if fd.client != nil {
			fd.client.CloseIdleConnections()
		}
		return err
	}
	defer fd.client.CloseIdleConnections()
	defer func() {
		if fd.File != nil {
			fd.File.Close()
		}
		if err != nil {
			os.Remove(fd.tempName)
			os.Remove(fd.FileName)
		}
	}()
	fd.createDownloadTasks()
	done := make(chan struct{})
	var progressWG sync.WaitGroup
	progressWG.Add(1)
	go func() {
		defer progressWG.Done()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if fd.progressCallback != nil {
					fd.progressCallback(float64(fd.downloaded.Load()), float64(fd.total.Load()), 0, 0)
				}
			}
		}
	}()
	err = fd.startDownload()
	if err != nil && fd.IsMultiPart && fd.ctx.Err() == nil {
		fd.IsMultiPart = false
		fd.downloaded.Store(0)
		fd.createDownloadTasks()
		err = fd.startDownload()
	}
	close(done)
	progressWG.Wait()
	if err != nil {
		return err
	}
	if err = fd.ctx.Err(); err != nil {
		return err
	}
	if err = fd.File.Sync(); err != nil {
		return err
	}
	if err = fd.File.Close(); err != nil {
		return err
	}
	fd.File = nil
	err = os.Rename(fd.tempName, fd.FileName)
	return err
}
func (fd *FileDownloader) Cancel() { fd.cancelFunc() }
func progressMessage(downloaded, total float64) string {
	if total <= 0 {
		return strconv.FormatInt(int64(downloaded), 10) + " bytes"
	}
	return strconv.Itoa(int(downloaded*100/total)) + "%"
}

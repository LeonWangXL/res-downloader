package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"res-downloader/core/shared"
	"strings"
	"sync"
	"time"
)

type Resource struct {
	mediaMark    sync.Map
	previewMedia sync.Map
	previewKeys  sync.Map
	taskMu       sync.Mutex
	jobs         map[string]*resourceJob
	queue        []*resourceJob
	active       int
	closing      bool
	workers      sync.WaitGroup
	resType      map[string]bool
	resTypeMux   sync.RWMutex
}

func initResource() *Resource {
	if resourceOnce == nil {
		resourceOnce = &Resource{jobs: make(map[string]*resourceJob)}
		resourceOnce.resType = resourceOnce.buildResType(configSnapshot().MimeMap)
	}
	return resourceOnce
}

func (r *Resource) buildResType(mime map[string]MimeInfo) map[string]bool {
	t := map[string]bool{
		"all": true,
	}

	for _, item := range mime {
		if _, ok := t[item.Type]; !ok {
			t[item.Type] = true
		}
	}

	return t
}

func (r *Resource) mediaIsMarked(key string) bool {
	_, loaded := r.mediaMark.Load(key)
	return loaded
}

func (r *Resource) markMedia(key string) bool {
	_, loaded := r.mediaMark.LoadOrStore(key, true)
	return !loaded
}

func (r *Resource) getResType(key string) (bool, bool) {
	r.resTypeMux.RLock()
	value, ok := r.resType[key]
	r.resTypeMux.RUnlock()
	return value, ok
}

func (r *Resource) setResType(n []string) {
	r.resTypeMux.Lock()
	for key := range r.resType {
		r.resType[key] = false
	}

	for _, value := range n {
		if _, ok := r.resType[value]; ok {
			r.resType[value] = true
		}
	}
	r.resTypeMux.Unlock()
}

func (r *Resource) clear() {
	r.mediaMark.Range(func(key, value interface{}) bool { r.mediaMark.Delete(key); return true })
	r.previewMedia.Range(func(key, value interface{}) bool { r.previewMedia.Delete(key); return true })
	r.previewKeys.Range(func(key, value interface{}) bool { r.previewKeys.Delete(key); return true })
}

func (r *Resource) delete(sign string) {
	r.mediaMark.Delete(sign)
	r.previewMedia.Range(func(key, value interface{}) bool {
		if info, ok := value.(shared.MediaInfo); ok && info.UrlSign == sign {
			r.previewMedia.Delete(key)
			r.previewKeys.Delete(key)
		}
		return true
	})
}

type resourceJob struct {
	media              shared.MediaInfo
	downloader         *FileDownloader
	decodeStr          string
	running, cancelled bool
	done               chan struct{}
}

func (r *Resource) download(media shared.MediaInfo, decodeStr string) error {
	cfg := configSnapshot()
	if cfg.SaveDirectory == "" {
		return errors.New("download directory is empty")
	}
	if media.Id == "" || len(media.Id) > 256 {
		return errors.New("invalid resource ID")
	}
	parsed, err := url.Parse(media.Url)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("invalid resource URL")
	}
	if media.Classify == "live" || media.Classify == "m3u8" {
		return errors.New("stream downloads are not supported")
	}
	if media.Suffix == "default" {
		media.Suffix = ".bin"
	}
	if media.Suffix == "" || !strings.HasPrefix(media.Suffix, ".") || strings.ContainsAny(media.Suffix, `/\:<>|?*`) {
		return errors.New("invalid resource suffix")
	}
	if decodeStr != "" {
		decoded, err := base64.StdEncoding.DecodeString(decodeStr)
		if err != nil || len(decoded) > 1024*1024 {
			return errors.New("invalid decryption data")
		}
	}
	name := shared.GetFileNameFromURL(media.Url)
	if name == "" {
		name = shared.Md5(media.Url)
	}
	if media.Description != "" {
		name = regexp.MustCompile(`[^\w\p{Han}]`).ReplaceAllString(media.Description, "")
		length := cfg.FilenameLen
		if length <= 0 {
			length = 10
		}
		chars := []rune(name)
		if len(chars) > length {
			name = string(chars[:length])
		}
		if name == "" {
			name = shared.Md5(media.Url)
		}
	}
	if cfg.FilenameTime {
		name += "_" + shared.GetCurrentDateTimeFormatted()
	}
	if !strings.HasSuffix(name, media.Suffix) {
		name += media.Suffix
	}
	media.SavePath = filepath.Join(cfg.SaveDirectory, name)
	if parsed.Hostname() == "qq.com" || strings.HasSuffix(parsed.Hostname(), ".qq.com") {
		query := parsed.Query()
		if cfg.Quality == 1 && query.Has("encfilekey") && query.Has("token") {
			query = url.Values{"encfilekey": {query.Get("encfilekey")}, "token": {query.Get("token")}}
		} else if cfg.Quality > 1 && media.OtherData["wx_file_formats"] != "" {
			formats := strings.Split(media.OtherData["wx_file_formats"], "#")
			quality := []string{formats[0], formats[len(formats)/2], formats[len(formats)-1]}
			query.Set("X-snsvideoflag", quality[cfg.Quality-2])
		}
		parsed.RawQuery = query.Encode()
	}
	headers, err := r.parseHeaders(media)
	if err != nil {
		return err
	}
	fd := NewFileDownloader(parsed.String(), media.SavePath, cfg.TaskNumber, headers)
	fd.config = cfg
	job := &resourceJob{media: media, downloader: fd, decodeStr: decodeStr, done: make(chan struct{})}
	job.media.Status = "pending"
	fd.progressCallback = func(downloaded, total float64, _ int, _ float64) {
		r.emitProgress(job, progressMessage(downloaded, total), shared.DownloadStatusRunning)
	}
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	if r.closing {
		return errors.New("application is closing")
	}
	if r.jobs == nil {
		r.jobs = make(map[string]*resourceJob)
	}
	for _, existing := range r.jobs {
		if existing.media.Id == media.Id || existing.media.Url == media.Url {
			return errors.New("resource is already queued or downloading")
		}
	}
	if len(r.jobs) >= 1000 {
		return errors.New("download queue is full")
	}
	r.jobs[media.Id] = job
	r.queue = append(r.queue, job)
	r.progressEventsEmit(job.media, "queued", "pending")
	r.scheduleLocked(cfg.DownNumber)
	return nil
}
func (r *Resource) schedule() {
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	r.scheduleLocked(configSnapshot().DownNumber)
}
func (r *Resource) scheduleLocked(limit int) {
	if limit < 1 {
		limit = 1
	}
	for !r.closing && r.active < limit && len(r.queue) > 0 {
		job := r.queue[0]
		r.queue = r.queue[1:]
		job.running = true
		job.media.Status = shared.DownloadStatusRunning
		r.active++
		r.workers.Add(1)
		r.progressEventsEmit(job.media, "starting", shared.DownloadStatusRunning)
		go r.runJob(job)
	}
}
func (r *Resource) runJob(job *resourceJob) {
	defer r.workers.Done()
	err := job.downloader.Start()
	if err == nil && job.decodeStr != "" {
		r.emitProgress(job, "decrypting in progress", shared.DownloadStatusRunning)
		err = r.decodeWxFile(job.downloader.FileName, job.decodeStr)
	}
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	job.media.SavePath = job.downloader.FileName
	if job.cancelled || errors.Is(err, context.Canceled) {
		job.media.SavePath = ""
		r.progressEventsEmit(job.media, "cancelled", shared.DownloadStatusReady)
	} else if err != nil {
		r.progressEventsEmit(job.media, err.Error(), shared.DownloadStatusError)
	} else {
		r.progressEventsEmit(job.media, "complete", shared.DownloadStatusDone)
	}
	delete(r.jobs, job.media.Id)
	r.active--
	close(job.done)
	r.scheduleLocked(configSnapshot().DownNumber)
}
func (r *Resource) emitProgress(job *resourceJob, message, status string) {
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	if job.cancelled {
		return
	}
	r.progressEventsEmit(job.media, message, status)
}
func (r *Resource) cancel(id string) error {
	r.taskMu.Lock()
	job, ok := r.jobs[id]
	if !ok {
		r.taskMu.Unlock()
		return errors.New("task not found")
	}
	job.cancelled = true
	job.downloader.Cancel()
	if !job.running {
		for i, item := range r.queue {
			if item == job {
				r.queue = append(r.queue[:i], r.queue[i+1:]...)
				break
			}
		}
		delete(r.jobs, id)
		job.media.SavePath = ""
		r.progressEventsEmit(job.media, "cancelled", shared.DownloadStatusReady)
		close(job.done)
	}
	r.taskMu.Unlock()
	select {
	case <-job.done:
		return nil
	case <-time.After(35 * time.Second):
		return errors.New("cancellation is still pending")
	}
}
func (r *Resource) taskSnapshot() []shared.MediaInfo {
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	items := make([]shared.MediaInfo, 0, len(r.jobs))
	for _, job := range r.jobs {
		items = append(items, job.media)
	}
	return items
}
func (r *Resource) shutdown() {
	r.taskMu.Lock()
	r.closing = true
	for _, job := range r.jobs {
		job.cancelled = true
		job.downloader.Cancel()
	}
	r.queue = nil
	r.taskMu.Unlock()
	r.workers.Wait()
}
func (r *Resource) updateResTypes(mime map[string]MimeInfo) {
	r.resTypeMux.Lock()
	defer r.resTypeMux.Unlock()
	next := r.buildResType(mime)
	for key := range next {
		next[key] = r.resType[key]
	}
	r.resType = next
}

func (r *Resource) parseHeaders(mediaInfo shared.MediaInfo) (map[string]string, error) {
	headers := make(map[string]string)

	if hh, ok := mediaInfo.OtherData["headers"]; ok {
		var tempHeaders map[string][]string
		if err := json.Unmarshal([]byte(hh), &tempHeaders); err != nil {
			return headers, fmt.Errorf("parse headers JSON err: %v", err)
		}

		for key, values := range tempHeaders {
			if len(values) > 0 {
				headers[key] = values[0]
			}
		}
	}

	return headers, nil
}

func (r *Resource) wxFileDecode(mediaInfo shared.MediaInfo, fileName, decodeStr string) (string, error) {
	sourceFile, err := os.Open(fileName)
	if err != nil {
		return "", err
	}
	defer sourceFile.Close()
	mediaInfo.SavePath = strings.TrimSuffix(fileName, filepath.Ext(fileName)) + "_decrypt.mp4"
	mediaInfo.SavePath = shared.GetUniqueFileName(mediaInfo.SavePath)

	destinationFile, err := os.OpenFile(mediaInfo.SavePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	defer destinationFile.Close()
	success := false
	defer func() {
		if !success {
			os.Remove(mediaInfo.SavePath)
		}
	}()

	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		return "", err
	}
	if err := destinationFile.Close(); err != nil {
		return "", err
	}
	err = r.decodeWxFile(mediaInfo.SavePath, decodeStr)
	if err != nil {
		return "", err
	}
	success = true
	return mediaInfo.SavePath, nil
}

func (r *Resource) progressEventsEmit(mediaInfo shared.MediaInfo, args ...string) {
	Status := shared.DownloadStatusError
	Message := "ok"

	if len(args) > 0 {
		Message = args[0]
	}
	if len(args) > 1 {
		Status = args[1]
	}

	httpServerOnce.send("downloadProgress", map[string]interface{}{
		"Id":       mediaInfo.Id,
		"Status":   Status,
		"SavePath": mediaInfo.SavePath,
		"Message":  Message,
	})
	return
}

func (r *Resource) decodeWxFile(fileName, decodeStr string) error {
	decodedBytes, err := base64.StdEncoding.DecodeString(decodeStr)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(fileName, os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	byteCount := len(decodedBytes)
	fileBytes := make([]byte, byteCount)
	n, err := file.Read(fileBytes)
	if err != nil && err != io.EOF {
		return err
	}

	if n < byteCount {
		return io.ErrUnexpectedEOF
	}

	xorResult := make([]byte, byteCount)
	for i := 0; i < byteCount; i++ {
		xorResult[i] = decodedBytes[i] ^ fileBytes[i]
	}
	_, err = file.Seek(0, 0)
	if err != nil {
		return err
	}

	_, err = file.Write(xorResult)
	if err != nil {
		return err
	}
	return nil
}

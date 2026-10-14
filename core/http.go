package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"res-downloader/core/shared"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type respData map[string]interface{}

type ResponseData struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type HttpServer struct {
	Host, Port    string
	server        *http.Server
	previewClient *http.Client
}

func initHttpServer() *HttpServer {
	if httpServerOnce == nil {
		httpServerOnce = &HttpServer{}
	}
	return httpServerOnce
}

func (h *HttpServer) start() error {
	cfg := configSnapshot()
	h.Host, h.Port = cfg.Host, cfg.Port
	listener, err := net.Listen("tcp", net.JoinHostPort(h.Host, h.Port))
	if err != nil {
		return fmt.Errorf("local service cannot listen on %s: %w", listenerAddress(h), err)
	}
	h.previewClient = (&FileDownloader{}).buildClient()
	h.previewClient.Transport.(*http.Transport).Proxy = func(r *http.Request) (*url.URL, error) {
		cfg := configSnapshot()
		if cfg.DownloadProxy && cfg.UpstreamProxy != "" {
			return url.Parse(cfg.UpstreamProxy)
		}
		return nil, nil
	}
	h.server = &http.Server{ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, port, err := net.SplitHostPort(r.Host)
		isLocal := err == nil && port == h.Port && (host == "localhost" || net.ParseIP(host).IsLoopback())
		if isLocal && HandleApi(w, r) {
			return
		}
		if isLocal {
			http.NotFound(w, r)
			return
		}
		proxyOnce.Proxy.ServeHTTP(w, r)
	})}
	go func() {
		if err := h.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			globalLogger.Esg(err, "local service stopped")
		}
	}()
	return nil
}
func listenerAddress(h *HttpServer) string { return net.JoinHostPort(h.Host, h.Port) }
func (h *HttpServer) stop() {
	if h.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = h.server.Shutdown(ctx)
	}
	if h.previewClient != nil {
		h.previewClient.CloseIdleConnections()
	}
}

func (h *HttpServer) downCert(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-x509-ca-data")
	w.Header().Set("Content-Disposition", "attachment;filename=res-downloader-public.crt")
	w.Header().Set("Content-Transfer-Encoding", "binary")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(appOnce.PublicCrt)))
	w.WriteHeader(http.StatusOK)
	io.Copy(w, io.NopCloser(bytes.NewReader(appOnce.PublicCrt)))
}

func (h *HttpServer) preview(w http.ResponseWriter, r *http.Request) {
	realURL := r.URL.Query().Get("url")
	if realURL == "" {
		http.Error(w, "Missing 'url' parameter", http.StatusBadRequest)
		return
	}
	parsedURL, err := url.Parse(realURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}
	resp, err := h.fetchPreview(r.Context(), realURL, r.Header.Get("Range"))
	if err != nil {
		http.Error(w, "Failed to fetch the resource", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	if key, ok := resourceOnce.previewKeys.Load(realURL); ok && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent) {
		offset := int64(0)
		if resp.StatusCode == http.StatusPartialContent {
			rangeValue := strings.TrimPrefix(resp.Header.Get("Content-Range"), "bytes ")
			offset, err = strconv.ParseInt(strings.SplitN(rangeValue, "-", 2)[0], 10, 64)
			if err != nil || offset < 0 {
				http.Error(w, "Invalid encrypted preview range", http.StatusBadGateway)
				return
			}
		}
		resp.Body = &previewDecryptReader{ReadCloser: resp.Body, key: key.([]byte), offset: offset}
		resp.Header.Set("Cache-Control", "no-store")
		resp.Header.Del("ETag")
		resp.Header.Del("Content-MD5")
		resp.Header.Set("Content-Type", "video/mp4")
	}
	// A playlist's relative segments and keys must use the same authenticated proxy.
	if resp.StatusCode == http.StatusOK && (strings.HasSuffix(strings.ToLower(resp.Request.URL.Path), ".m3u8") || strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "mpegurl")) {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
		if err != nil || len(body) > 2*1024*1024 {
			http.Error(w, "Failed to read playlist", http.StatusBadGateway)
			return
		}
		body, err = h.rewritePlaylist(body, resp.Request.URL, realURL)
		if err != nil {
			http.Error(w, "Invalid playlist URL", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(body)
		return
	}

	for k, v := range resp.Header {
		if strings.HasPrefix(strings.ToLower(k), "access-control-") || strings.EqualFold(k, "Connection") || strings.EqualFold(k, "Transfer-Encoding") {
			continue
		}
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	w.WriteHeader(resp.StatusCode)

	_, err = io.Copy(w, resp.Body)
	if err != nil {
		globalLogger.Esg(err, "serve preview")
	}
	return
}

func (h *HttpServer) preparePreview(w http.ResponseWriter, r *http.Request) {
	var data struct {
		shared.MediaInfo
		DecodeStr string `json:"decodeStr"`
		Check     bool   `json:"check"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		h.error(w, "Invalid preview source")
		return
	}
	parsed, err := url.Parse(data.Url)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		h.error(w, "Invalid preview URL")
		return
	}
	var key []byte
	if data.DecodeStr != "" {
		key, err = base64.StdEncoding.DecodeString(data.DecodeStr)
		if err != nil || len(key) != 131072 {
			h.error(w, "Invalid preview key")
			return
		}
	}
	// Preserve freshly captured login headers; restored history can supply its remaining headers.
	if _, loaded := resourceOnce.previewMedia.Load(data.Url); !loaded {
		resourceOnce.previewMedia.Store(data.Url, data.MediaInfo)
	}
	if key != nil {
		resourceOnce.previewKeys.Store(data.Url, key)
	} else {
		resourceOnce.previewKeys.Delete(data.Url)
	}
	if data.Check {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		resp, err := h.fetchPreview(ctx, data.Url, "bytes=0-4095")
		if err != nil {
			h.error(w, "Preview connection failed", respData{"reason": "network"})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			h.error(w, "Preview upstream rejected request", respData{"reason": "http", "status": resp.StatusCode})
			return
		}
		if resp.StatusCode == http.StatusPartialContent && !strings.HasPrefix(resp.Header.Get("Content-Range"), "bytes 0-") {
			h.error(w, "Preview returned wrong range", respData{"reason": "range"})
			return
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err != nil || len(body) == 0 {
			h.error(w, "Preview read failed", respData{"reason": "network"})
			return
		}
		text := strings.ToLower(strings.TrimSpace(string(body)))
		if strings.HasPrefix(text, "<!doctype html") || strings.HasPrefix(text, "<html") || strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
			h.error(w, "Preview returned a webpage", respData{"reason": "non_media"})
			return
		}
		if key != nil {
			if mp4Prefix(body) {
				// Some CDN variants already return plaintext despite carrying a decode key.
				resourceOnce.previewKeys.Delete(data.Url)
			} else {
				for i := 0; i < len(body) && i < len(key); i++ {
					body[i] ^= key[i]
				}
				if !mp4Prefix(body) {
					h.error(w, "Preview decryption failed", respData{"reason": "decrypt"})
					return
				}
			}
		}
		h.success(w, respData{"status": resp.StatusCode, "contentType": resp.Header.Get("Content-Type")})
		return
	}
	h.success(w)
}

func mp4Prefix(body []byte) bool {
	if len(body) < 8 {
		return false
	}
	switch string(body[4:8]) {
	case "ftyp", "styp", "moof":
		return true
	}
	return false
}

type previewDecryptReader struct {
	io.ReadCloser
	key    []byte
	offset int64
}

func (r *previewDecryptReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	for i := 0; i < n && r.offset+int64(i) < int64(len(r.key)); i++ {
		p[i] ^= r.key[r.offset+int64(i)]
	}
	r.offset += int64(n)
	return n, err
}

func (h *HttpServer) rewritePlaylist(body []byte, base *url.URL, source string) ([]byte, error) {
	localURL := func(raw string) (string, error) {
		relative, err := url.Parse(raw)
		if err != nil {
			return "", err
		}
		absolute := base.ResolveReference(relative)
		if absolute.Scheme != "http" && absolute.Scheme != "https" {
			return "", fmt.Errorf("unsupported playlist scheme")
		}
		if info, ok := resourceOnce.previewMedia.Load(source); ok {
			media := info.(shared.MediaInfo)
			media.Url = absolute.String()
			resourceOnce.previewMedia.Store(media.Url, media)
		}
		return "http://" + net.JoinHostPort(h.Host, h.Port) + "/api/preview?" + url.Values{"url": {absolute.String()}, "token": {appOnce.SessionToken}}.Encode(), nil
	}
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			rewritten, err := localURL(line)
			if err != nil {
				return nil, err
			}
			lines[i] = rewritten
		} else {
			// HLS URI attributes include encryption keys, maps and alternate renditions.
			const marker = `URI="`
			cursor := 0
			for {
				start := strings.Index(line[cursor:], marker)
				if start < 0 {
					break
				}
				start += cursor + len(marker)
				end := strings.Index(line[start:], `"`)
				if end < 0 {
					return nil, fmt.Errorf("unterminated playlist URI")
				}
				end += start
				rewritten, err := localURL(line[start:end])
				if err != nil {
					return nil, err
				}
				line = line[:start] + rewritten + line[end:]
				cursor = start + len(rewritten) + 1
			}
			lines[i] = line
		}
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func (h *HttpServer) send(t string, data interface{}) {
	jsonData, err := json.Marshal(map[string]interface{}{
		"type": t,
		"data": data,
	})
	if err != nil {
		fmt.Println("Error converting map to JSON:", err)
		return
	}
	if t == "newResources" {
		if info, ok := data.(shared.MediaInfo); ok {
			resourceOnce.previewMedia.Store(info.Url, info)
		}
	}
	if appOnce != nil && appOnce.ctx != nil {
		runtime.EventsEmit(appOnce.ctx, "event", string(jsonData))
	}
}

func (h *HttpServer) writeJson(w http.ResponseWriter, data *ResponseData) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	err := json.NewEncoder(w).Encode(data)
	if err != nil {
		globalLogger.Err(err)
	}
}

func (h *HttpServer) error(w http.ResponseWriter, args ...interface{}) {
	message := "ok"
	var data interface{}

	if len(args) > 0 {
		message = args[0].(string)
	}
	if len(args) > 1 {
		data = args[1]
	}
	h.writeJson(w, h.buildResp(0, message, data))
}

func (h *HttpServer) success(w http.ResponseWriter, args ...interface{}) {
	message := "ok"
	var data interface{}

	if len(args) > 0 {
		data = args[0]
	}

	if len(args) > 1 {
		message = args[1].(string)
	}
	h.writeJson(w, h.buildResp(1, message, data))
}

func (h *HttpServer) buildResp(code int, message string, data interface{}) *ResponseData {
	return &ResponseData{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

func (h *HttpServer) openDirectoryDialog(w http.ResponseWriter, r *http.Request) {
	folder, err := runtime.OpenDirectoryDialog(appOnce.ctx, runtime.OpenDialogOptions{
		DefaultDirectory: "",
		Title:            "Select a folder",
	})
	if err != nil {
		h.error(w, err.Error())
		return
	}
	h.success(w, respData{
		"folder": folder,
	})
}

func (h *HttpServer) openFileDialog(w http.ResponseWriter, r *http.Request) {
	filePath, err := runtime.OpenFileDialog(appOnce.ctx, runtime.OpenDialogOptions{
		Filters: []runtime.FileFilter{
			{
				DisplayName: "Videos (*.mov;*.mp4)",
				Pattern:     "*.mp4",
			},
		},
		Title: "Select a file",
	})
	if err != nil {
		h.error(w, err.Error())
		return
	}
	h.success(w, respData{
		"file": filePath,
	})
}

func (h *HttpServer) openFolder(w http.ResponseWriter, r *http.Request) {
	var data struct {
		FilePath string `json:"filePath"`
	}
	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil || data.FilePath == "" {
		h.error(w, "invalid file path")
		return
	}

	err = shared.OpenFolder(data.FilePath)
	if err != nil {
		globalLogger.Err(err)
		h.error(w, err.Error())
		return
	}
	h.success(w)
	return
}

func (h *HttpServer) install(w http.ResponseWriter, r *http.Request) {
	if appOnce.isInstall() {
		h.success(w, respData{
			"isPass": systemOnce.password() == "",
		})
		return
	}

	out, err := appOnce.installCert()
	if err != nil {
		h.error(w, err.Error()+"\n"+out, respData{
			"isPass": systemOnce.password() == "",
		})
		return
	}

	h.success(w, respData{
		"isPass": systemOnce.password() == "",
	})
}

func (h *HttpServer) setSystemPassword(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Password string `json:"password"`
		IsCache  bool   `json:"isCache"`
	}
	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		h.error(w, err.Error())
		return
	}
	systemOnce.SetPassword(data.Password, data.IsCache)
	h.success(w)
}

func (h *HttpServer) openSystemProxy(w http.ResponseWriter, r *http.Request) {
	err := appOnce.OpenSystemProxy()
	if err != nil {
		h.error(w, err.Error(), respData{
			"value": appOnce.proxyEnabled(),
		})
		return
	}
	h.success(w, respData{
		"value": appOnce.proxyEnabled(),
	})
}

func (h *HttpServer) unsetSystemProxy(w http.ResponseWriter, r *http.Request) {
	err := appOnce.UnsetSystemProxy()
	if err != nil {
		h.error(w, err.Error(), respData{
			"value": appOnce.proxyEnabled(),
		})
		return
	}
	h.success(w, respData{
		"value": appOnce.proxyEnabled(),
	})
}

func (h *HttpServer) isProxy(w http.ResponseWriter, r *http.Request) {
	h.success(w, respData{
		"value": appOnce.proxyEnabled(),
	})
}

func (h *HttpServer) appInfo(w http.ResponseWriter, r *http.Request) {
	h.success(w, respData{"AppName": appOnce.AppName, "Version": appOnce.Version, "IsProxy": appOnce.proxyEnabled()})
}

func (h *HttpServer) getConfig(w http.ResponseWriter, r *http.Request) {
	h.success(w, configSnapshot())
}

func (h *HttpServer) setConfig(w http.ResponseWriter, r *http.Request) {
	var data Config
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		h.error(w, err.Error())
		return
	}
	if err := globalConfig.setConfig(data); err != nil {
		h.error(w, err.Error())
		return
	}
	h.success(w)
}

func (h *HttpServer) setType(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Type string `json:"type"`
	}
	err := json.NewDecoder(r.Body).Decode(&data)
	if err == nil {
		if data.Type != "" {
			resourceOnce.setResType(strings.Split(data.Type, ","))
		} else {
			resourceOnce.setResType([]string{})
		}
	}

	h.success(w)
}

func (h *HttpServer) clear(w http.ResponseWriter, r *http.Request) {
	resourceOnce.clear()
	h.success(w)
}

func (h *HttpServer) delete(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Sign []string `json:"sign"`
	}
	err := json.NewDecoder(r.Body).Decode(&data)
	if err == nil && len(data.Sign) > 0 {
		for _, v := range data.Sign {
			resourceOnce.delete(v)
		}
	}
	h.success(w)
}

func (h *HttpServer) download(w http.ResponseWriter, r *http.Request) {
	var data struct {
		shared.MediaInfo
		DecodeStr string `json:"decodeStr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		h.error(w, err.Error())
		return
	}
	if err := resourceOnce.download(data.MediaInfo, data.DecodeStr); err != nil {
		h.error(w, err.Error())
		return
	}
	h.success(w)
}

func (h *HttpServer) cancel(w http.ResponseWriter, r *http.Request) {
	var data struct {
		shared.MediaInfo
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		h.error(w, err.Error())
		return
	}

	err := resourceOnce.cancel(data.Id)
	if err != nil {
		h.error(w, err.Error())
		return
	}
	h.success(w)
}

func (h *HttpServer) wxFileDecode(w http.ResponseWriter, r *http.Request) {
	var data struct {
		shared.MediaInfo
		Filename  string `json:"filename"`
		DecodeStr string `json:"decodeStr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		h.error(w, err.Error())
		return
	}
	savePath, err := resourceOnce.wxFileDecode(data.MediaInfo, data.Filename, data.DecodeStr)
	if err != nil {
		h.error(w, err.Error())
		return
	}
	h.success(w, respData{
		"save_path": savePath,
	})
}

func (h *HttpServer) batchExport(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		h.error(w, err.Error())
		return
	}
	fileName := filepath.Join(configSnapshot().SaveDirectory, "res-downloader-"+shared.GetCurrentDateTimeFormatted()+".txt")
	err := os.WriteFile(fileName, []byte(data.Content), 0644)
	if err != nil {
		h.error(w, err.Error())
		return
	}

	_ = shared.OpenFolder(fileName)
	h.success(w, respData{
		"file_name": fileName,
	})
}

func (h *HttpServer) tasks(w http.ResponseWriter, r *http.Request) {
	h.success(w, resourceOnce.taskSnapshot())
}

func (h *HttpServer) fetchPreview(ctx context.Context, realURL, rangeHeader string) (*http.Response, error) {
	parsedURL, err := url.Parse(realURL)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, "GET", realURL, nil)
	if err != nil {
		return nil, err
	}

	if rangeHeader != "" {
		request.Header.Set("Range", rangeHeader)
	}

	if info, ok := resourceOnce.previewMedia.Load(realURL); ok {
		headers, err := resourceOnce.parseHeaders(info.(shared.MediaInfo))
		if err == nil {
			fd := NewFileDownloader(realURL, "", 1, headers)
			fd.setHeaders(request)
		}
	}
	if request.Header.Get("User-Agent") == "" {
		request.Header.Set("User-Agent", configSnapshot().UserAgent)
	}
	if request.Header.Get("Referer") == "" {
		request.Header.Set("Referer", parsedURL.Scheme+"://"+parsedURL.Host+"/")
	}
	return h.previewClient.Do(request)
}

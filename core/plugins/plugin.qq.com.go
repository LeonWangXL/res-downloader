package plugins

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/elazarl/goproxy"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"io"
	"net/http"
	"regexp"
	"res-downloader/core/shared"
	"strconv"
	"strings"
)

var qqMediaRegex = regexp.MustCompile(`get\s*media\(\)\{`)
var qqCommentRegex = regexp.MustCompile(`async\s*finderGetCommentDetail\((\w+)\)\s*\{return(.*?)\s*}\s*async`)

type QqPlugin struct {
	bridge *shared.Bridge
}

func (p *QqPlugin) SetBridge(bridge *shared.Bridge) {
	p.bridge = bridge
}

func (p *QqPlugin) Domains() []string {
	return []string{"qq.com"}
}

func (p *QqPlugin) OnRequest(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	if strings.Contains(r.Host, "qq.com") && strings.Contains(r.URL.Path, "/res-downloader/wechat") {
		if p.bridge.GetConfig("WxAction").(bool) && r.URL.Query().Get("type") == "1" {
			return p.handleWechatRequest(r, ctx)
		} else if !p.bridge.GetConfig("WxAction").(bool) && r.URL.Query().Get("type") == "2" {
			return p.handleWechatRequest(r, ctx)
		} else {
			return r, p.buildEmptyResponse(r)
		}
	}

	if strings.HasSuffix(r.URL.Hostname(), "res.wx.qq.com") || strings.HasSuffix(r.URL.Hostname(), "channels.weixin.qq.com") {
		r.Header.Set("Accept-Encoding", "identity")
	}
	return nil, nil
}

func (p *QqPlugin) OnResponse(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		return nil
	}

	host := resp.Request.URL.Hostname()
	Path := resp.Request.URL.Path

	classify, _ := p.bridge.TypeSuffix(resp.Header.Get("Content-Type"))
	if classify == "video" && strings.HasSuffix(host, "finder.video.qq.com") {
		if strings.Contains(resp.Request.Header.Get("Origin"), "mp.weixin.qq.com") {
			return nil
		}
		return resp
	}

	if strings.HasSuffix(host, "channels.weixin.qq.com") &&
		(strings.Contains(Path, "/web/pages/feed") || strings.Contains(Path, "/web/pages/home")) {
		return p.replaceWxJsContent(resp, ".js\"", ".js?v="+p.v()+"\"")
	}

	if strings.HasSuffix(host, "res.wx.qq.com") {
		respTemp := resp
		is := false
		if strings.HasSuffix(respTemp.Request.URL.RequestURI(), ".js?v="+p.v()) {
			respTemp = p.replaceWxJsContent(respTemp, ".js\"", ".js?v="+p.v()+"\"")
			is = true
		}

		if strings.Contains(Path, "web/web-finder/res/js/virtual_svg-icons-register.publish") {
			body, err := readRewriteBody(respTemp)
			if err != nil {
				return respTemp
			}
			bodyStr := string(body)
			newBody := qqMediaRegex.
				ReplaceAllString(bodyStr, `
							get media(){
								if(this.objectDesc){
									fetch("https://wxapp.tc.qq.com/res-downloader/wechat?type=1", {
									  method: "POST",
									  mode: "no-cors",
									  body: JSON.stringify(this.objectDesc),
									});
								};
			
			`)

			newBody = qqCommentRegex.
				ReplaceAllString(newBody, `
							async finderGetCommentDetail($1) {
								var res = await$2;
								if (res?.data?.object?.objectDesc) {
									fetch("https://wxapp.tc.qq.com/res-downloader/wechat?type=2", {
									  method: "POST",
									  mode: "no-cors",
									  body: JSON.stringify(res.data.object.objectDesc),
									});
								}
								return res;
							}async
			`)
			newBodyBytes := []byte(newBody)
			respTemp.Body = io.NopCloser(bytes.NewBuffer(newBodyBytes))
			respTemp.ContentLength = int64(len(newBodyBytes))
			respTemp.Header.Set("Content-Length", fmt.Sprintf("%d", len(newBodyBytes)))
			return respTemp
		}
		if is {
			return respTemp
		}
	}

	return nil
}

func (p *QqPlugin) handleWechatRequest(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024+1))
	if err != nil || len(body) > 2*1024*1024 {
		return r, p.buildEmptyResponse(r)
	}

	go p.handleMedia(body)

	return r, p.buildEmptyResponse(r)
}

func (p *QqPlugin) handleMedia(body []byte) {
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return
	}

	mediaArr, ok := result["media"].([]interface{})
	if !ok || len(mediaArr) == 0 {
		return
	}

	firstMedia, ok := mediaArr[0].(map[string]interface{})
	if !ok {
		return
	}

	rawUrl, ok := firstMedia["url"].(string)
	if !ok || rawUrl == "" {
		return
	}

	urlSign := shared.Md5(rawUrl)
	if p.bridge.MediaIsMarked(urlSign) {
		return
	}

	id, err := gonanoid.New()
	if err != nil {
		id = urlSign
	}

	res := shared.MediaInfo{
		Id:          id,
		Url:         rawUrl,
		UrlSign:     urlSign,
		CoverUrl:    "",
		Size:        0,
		Domain:      shared.GetTopLevelDomain(rawUrl),
		Classify:    "video",
		Suffix:      ".mp4",
		Status:      shared.DownloadStatusReady,
		SavePath:    "",
		DecodeKey:   "",
		OtherData:   map[string]string{},
		Description: "",
		ContentType: "video/mp4",
	}

	if mediaType, ok := firstMedia["mediaType"].(float64); ok && mediaType == 9 {
		res.Classify = "image"
		res.Suffix = ".png"
		res.ContentType = "image/png"
	}

	isAll, _ := p.bridge.GetResType("all")
	isImage, _ := p.bridge.GetResType("image")
	if res.Classify == "image" && !isImage && !isAll {
		return
	}

	isVideo, _ := p.bridge.GetResType("video")
	if res.Classify == "video" && !isVideo && !isAll {
		return
	}

	if urlToken, ok := firstMedia["urlToken"].(string); ok {
		res.Url += urlToken
	}

	switch size := firstMedia["fileSize"].(type) {
	case float64:
		res.Size = size
	case string:
		if value, err := strconv.ParseFloat(size, 64); err == nil {
			res.Size = value
		}
	}

	if coverUrl, ok := firstMedia["coverUrl"].(string); ok {
		res.CoverUrl = coverUrl
	}

	if decodeKey, ok := firstMedia["decodeKey"].(string); ok {
		res.DecodeKey = decodeKey
	}

	if desc, ok := result["description"].(string); ok {
		res.Description = desc
	}

	if spec, ok := firstMedia["spec"].([]interface{}); ok {
		var fileFormats []string
		for _, item := range spec {
			if m, ok := item.(map[string]interface{}); ok {
				if format, ok := m["fileFormat"].(string); ok {
					fileFormats = append(fileFormats, format)
				}
			}
		}
		res.OtherData["wx_file_formats"] = strings.Join(fileFormats, "#")
	}

	if !p.bridge.MarkMedia(urlSign) {
		return
	}

	go func(res shared.MediaInfo) {
		p.bridge.Send("newResources", res)
	}(res)
}

func (p *QqPlugin) buildEmptyResponse(r *http.Request) *http.Response {
	body := "The content does not exist"
	resp := &http.Response{
		Status:        http.StatusText(http.StatusOK),
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       r,
	}
	resp.Header.Set("Content-Type", "text/plain; charset=utf-8")
	return resp
}

func (p *QqPlugin) replaceWxJsContent(resp *http.Response, old, new string) *http.Response {
	body, err := readRewriteBody(resp)
	if err != nil {
		return resp
	}
	bodyString := string(body)
	newBodyString := strings.ReplaceAll(bodyString, old, new)
	newBodyBytes := []byte(newBodyString)
	resp.Body = io.NopCloser(bytes.NewBuffer(newBodyBytes))
	resp.ContentLength = int64(len(newBodyBytes))
	resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(newBodyBytes)))
	return resp
}

func (p *QqPlugin) v() string {
	return p.bridge.GetVersion()
}

func readRewriteBody(resp *http.Response) ([]byte, error) {
	original := resp.Body
	raw, err := io.ReadAll(io.LimitReader(original, 8*1024*1024+1))
	// Preserve the response on a read/decode failure or when the rewrite limit is exceeded.
	resp.Body = &replayBody{Reader: io.MultiReader(bytes.NewReader(raw), original), closer: original}
	if err != nil {
		return nil, err
	}
	if len(raw) > 8*1024*1024 {
		return nil, errors.New("script rewrite limit exceeded")
	}
	var reader io.Reader = bytes.NewReader(raw)
	var decoded io.ReadCloser
	switch strings.ToLower(resp.Header.Get("Content-Encoding")) {
	case "", "identity":
	case "gzip":
		decoded, err = gzip.NewReader(reader)
	case "deflate":
		decoded, err = zlib.NewReader(reader)
	default:
		return nil, errors.New("unsupported script encoding")
	}
	if err != nil {
		return nil, err
	}
	if decoded != nil {
		defer decoded.Close()
		reader = decoded
	}
	body, err := io.ReadAll(io.LimitReader(reader, 8*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 8*1024*1024 {
		return nil, errors.New("decoded script limit exceeded")
	}
	original.Close()
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("ETag")
	resp.Header.Del("Content-MD5")
	return body, nil
}

type replayBody struct {
	io.Reader
	closer io.Closer
}

func (r *replayBody) Close() error { return r.closer.Close() }

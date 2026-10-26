package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"time"

	assets "phantomframe/server"
)

func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func upstreamClient() *http.Client {
	return &http.Client{Transport: upstreamTransport{http.DefaultTransport}, Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("供应商请求不允许重定向") }}
}
func boundedBody(res *http.Response, limit int64) ([]byte, error) {
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("供应商请求失败（HTTP %d）", res.StatusCode)
	}
	if res.ContentLength > limit {
		return nil, fmt.Errorf("供应商文件超过大小限制")
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("供应商文件超过大小限制")
	}
	return b, nil
}
func pathValue(v any, path string) any {
	for _, k := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}
func fallback(v any, s string) string {
	if str(v) == "" {
		return s
	}
	return str(v)
}
func first(values ...any) any {
	for _, v := range values {
		if v != nil && str(v) != "" {
			return v
		}
	}
	return nil
}

type providerRequest struct {
	ctx       context.Context
	base, key string
}

func (p providerRequest) bytes(path string, body io.Reader, contentType string) (*http.Response, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "..") || strings.Contains(path, "\\") {
		return nil, fmt.Errorf("供应商接口路径无效")
	}
	method := "GET"
	if body != nil {
		method = "POST"
	}
	r, e := http.NewRequestWithContext(p.ctx, method, p.base+path, body)
	if e != nil {
		return nil, e
	}
	r.Header.Set("Authorization", "Bearer "+p.key)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return upstreamClient().Do(r)
}
func (p providerRequest) request(path string, body io.Reader, contentType string) (Row, error) {
	res, e := p.bytes(path, body, contentType)
	if e != nil {
		return nil, e
	}
	b, e := boundedBody(res, 16*1024*1024)
	if e != nil {
		return nil, e
	}
	var v Row
	if e = json.Unmarshal(b, &v); e != nil {
		return nil, fmt.Errorf("供应商响应不是有效 JSON")
	}
	return v, nil
}
func (a *App) form(fields Row, references []Row, field string) (*bytes.Buffer, string, error) {
	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	for k, v := range fields {
		if e := w.WriteField(k, str(v)); e != nil {
			return nil, "", e
		}
	}
	for _, asset := range references {
		header := make(textproto.MIMEHeader)
		name := strings.ReplaceAll(strings.ReplaceAll(str(asset["name"]), "\"", ""), "\r", "")
		name = strings.ReplaceAll(name, "\n", "")
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, name))
		header.Set("Content-Type", str(asset["mime"]))
		part, e := w.CreatePart(header)
		if e != nil {
			return nil, "", e
		}
		file, e := os.Open(a.filePath(str(asset["file"])))
		if e != nil {
			return nil, "", e
		}
		_, e = io.Copy(part, io.LimitReader(file, 30*1024*1024+1))
		file.Close()
		if e != nil {
			return nil, "", e
		}
	}
	if e := w.Close(); e != nil {
		return nil, "", e
	}
	return buf, w.FormDataContentType(), nil
}
func outputClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, e
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("供应商文件地址不可访问")
		}
		for _, v := range ips {
			ip := v.IP
			if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				return nil, fmt.Errorf("供应商文件地址不可访问")
			}
		}
		dialer := net.Dialer{Timeout: 10 * time.Second}
		var last error
		for _, ip := range ips {
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		return nil, last
	}
	return &http.Client{Transport: upstreamTransport{transport}, Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("供应商文件不允许重定向") }}
}
func (a *App) demo(ctx context.Context, j Row) ([]Row, error) {
	if e := wait(ctx, 1200*time.Millisecond); e != nil {
		return nil, e
	}
	snapshot := object(j["provider_snapshot"])
	if snapshot["type"] == "video" || snapshot["type"] == "upscale" {
		o, e := a.saveAsset(str(j["user_id"]), str(j["id"]), "演示视频 · 非模型生成.webm", "video/webm", "webm", assets.DemoVideo)
		return []Row{o}, e
	}
	if snapshot["type"] == "text" {
		out, e := a.saveAsset(str(j["user_id"]), str(j["id"]), "演示文本.txt", "text/plain", "txt", []byte("本地演示 · 非 AI 模型生成\n"+str(j["prompt"])))
		return []Row{out}, e
	}
	if snapshot["type"] == "audio" {
		var wav bytes.Buffer
		wav.WriteString("RIFF")
		_ = binary.Write(&wav, binary.LittleEndian, uint32(16036))
		wav.WriteString("WAVEfmt ")
		_ = binary.Write(&wav, binary.LittleEndian, uint32(16))
		_ = binary.Write(&wav, binary.LittleEndian, uint16(1))
		_ = binary.Write(&wav, binary.LittleEndian, uint16(1))
		_ = binary.Write(&wav, binary.LittleEndian, uint32(8000))
		_ = binary.Write(&wav, binary.LittleEndian, uint32(16000))
		_ = binary.Write(&wav, binary.LittleEndian, uint16(2))
		_ = binary.Write(&wav, binary.LittleEndian, uint16(16))
		wav.WriteString("data")
		_ = binary.Write(&wav, binary.LittleEndian, uint32(16000))
		wav.Write(make([]byte, 16000))
		out, e := a.saveAsset(str(j["user_id"]), str(j["id"]), "演示静音音频.wav", "audio/wav", "wav", wav.Bytes())
		return []Row{out}, e
	}
	prompt := []rune(str(j["prompt"]))
	if len(prompt) > 38 {
		prompt = prompt[:38]
	}
	p := object(j["params"])
	width, height := 1200, 800
	ratio := strings.Split(fallback(first(p["aspectRatio"], p["aspect_ratio"]), "16:9"), ":")
	if len(ratio) == 2 && num(ratio[0]) > 0 && num(ratio[1]) > 0 {
		height = int(1200 * num(ratio[1]) / num(ratio[0]))
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 1200 800"><defs><linearGradient id="b" x2="1" y2="1"><stop stop-color="#082e29"/><stop offset="1" stop-color="#392343"/></linearGradient><radialGradient id="o"><stop stop-color="#7ae3c6"/><stop offset="1" stop-color="#18324a"/></radialGradient></defs><rect width="1200" height="800" fill="url(#b)"/><circle cx="600" cy="340" r="230" fill="url(#o)"/><ellipse cx="600" cy="340" rx="420" ry="120" fill="none" stroke="#8cddc3" transform="rotate(-25 600 340)"/><text x="55" y="65" fill="#d1ffe9" font-size="18" font-family="sans-serif">PHANTOMFRAME / CREATIVE STUDIO</text><text x="55" y="685" fill="white" font-size="25" font-family="sans-serif">%s</text><text x="55" y="740" fill="#a8b9c5" font-size="17" font-family="sans-serif">演示预览 · 非 AI 模型生成 · 配置供应商后可生成真实作品</text></svg>`, width, height, html.EscapeString(string(prompt)))
	o, e := a.saveAsset(str(j["user_id"]), str(j["id"]), "演示图片 · 非模型生成.svg", "image/svg+xml", "svg", []byte(svg))
	return []Row{o}, e
}
func (a *App) upstream(ctx context.Context, j Row) ([]Row, error) {
	p := object(j["provider_snapshot"])
	config, params := object(p["config"]), object(j["params"])
	key, e := a.decrypt(str(p["api_key"]))
	if e != nil {
		return nil, fmt.Errorf("供应商密钥解密失败")
	}
	if key == "" {
		return nil, fmt.Errorf("供应商尚未配置 API Key")
	}
	ctx = upstreamContext(ctx, a, str(j["id"]), key)
	client := providerRequest{ctx, strings.TrimRight(str(p["base_url"]), "/"), key}
	refs := []Row{}
	fields := []Row{}
	for _, f := range array(p["parameters"]) {
		field := object(f)
		if field["type"] != "assets" {
			continue
		}
		fields = append(fields, field)
		for _, assetID := range array(params[str(field["name"])]) {
			asset := a.Store.One("SELECT * FROM assets WHERE id=? AND user_id=?", assetID, j["user_id"])
			if asset == nil {
				return nil, fmt.Errorf("供应商参考素材不存在")
			}
			refs = append(refs, asset)
		}
	}
	if isVendor(p["protocol"]) {
		return a.vendorUpstream(ctx, j, p, key, refs)
	}
	var result Row
	if p["protocol"] == "openai-image" {
		for _, ref := range refs {
			if ref["type"] != "image" {
				return nil, fmt.Errorf("图片协议只支持参考图片")
			}
		}
		ratio := str(first(params["aspectRatio"], params["aspect_ratio"]))
		size := "1536x1024"
		if ratio == "9:16" {
			size = "1024x1536"
		} else if ratio == "1:1" {
			size = "1024x1024"
		}
		body := Row{"model": p["upstreamModel"], "prompt": j["prompt"], "n": first(params["n"], 1), "size": first(params["size"], size)}
		if params["quality"] != nil {
			body["quality"] = params["quality"]
		}
		if len(refs) > 0 {
			form, ct, err := a.form(body, refs, "image[]")
			if err != nil {
				return nil, err
			}
			result, e = client.request(fallback(config["editPath"], "/images/edits"), form, ct)
		} else {
			result, e = client.request(fallback(config["submitPath"], "/images/generations"), strings.NewReader(jsonText(body)), "application/json")
		}
	} else if p["protocol"] == "openai-video" {
		if len(refs) > 1 || (len(refs) == 1 && refs[0]["type"] != "image") {
			return nil, fmt.Errorf("此视频协议最多支持一张首帧参考图")
		}
		size := "1280x720"
		if params["aspectRatio"] == "9:16" {
			size = "720x1280"
		}
		form, ct, err := a.form(Row{"model": p["upstreamModel"], "prompt": j["prompt"], "seconds": first(params["duration"], 8), "size": first(params["size"], size)}, refs, "input_reference")
		if err != nil {
			return nil, err
		}
		result, e = client.request(fallback(config["submitPath"], "/videos"), form, ct)
	} else if p["protocol"] == "json-async" {
		body := clone(params)
		body["model"] = p["upstreamModel"]
		body["prompt"] = j["prompt"]
		for _, f := range fields {
			values := []string{}
			for _, assetID := range array(params[str(f["name"])]) {
				asset := a.Store.One("SELECT * FROM assets WHERE id=? AND user_id=?", assetID, j["user_id"])
				b, err := os.ReadFile(a.filePath(str(asset["file"])))
				if err != nil {
					return nil, err
				}
				values = append(values, "data:"+str(asset["mime"])+";base64,"+base64.StdEncoding.EncodeToString(b))
			}
			body[str(f["name"])] = values
		}
		result, e = client.request(fallback(config["submitPath"], "/generate/"+str(p["type"])), strings.NewReader(jsonText(body)), "application/json")
	} else {
		return nil, fmt.Errorf("供应商协议不受支持")
	}
	if e != nil {
		return nil, e
	}
	if p["protocol"] != "openai-image" {
		upID := first(pathValue(result, fallback(config["idPath"], "id")), result["task_id"], pathValue(result, "data.id"), pathValue(result, "data.task_id"))
		if upID != nil {
			upID = str(upID)
			if len(str(upID)) > 255 {
				return nil, fmt.Errorf("供应商任务 ID 过长")
			}
			a.setUpstreamTaskID(j["id"], upID)
			complete := false
			interval := time.Duration(num(first(config["pollInterval"], 3000))) * time.Millisecond
			if interval < 20*time.Millisecond {
				interval = 20 * time.Millisecond
			}
			if interval > 30*time.Second {
				interval = 30 * time.Second
			}
			for i := 0; i < 180; i++ {
				state := strings.ToLower(str(first(pathValue(result, fallback(config["statusField"], "status")), pathValue(result, "data.status"))))
				switch state {
				case "completed", "succeeded", "success", "done":
					complete = true
				case "failed", "failure", "cancelled", "error":
					return nil, fmt.Errorf("供应商生成失败，请检查提示词或素材后重试")
				}
				if complete {
					break
				}
				if e = wait(ctx, interval); e != nil {
					return nil, e
				}
				path := "/tasks/{id}"
				if p["protocol"] == "openai-video" {
					path = "/videos/{id}"
				}
				path = strings.ReplaceAll(fallback(config["statusPath"], path), "{id}", url.PathEscape(str(upID)))
				result, e = client.request(path, nil, "")
				if e != nil {
					return nil, e
				}
			}
			if !complete {
				return nil, fmt.Errorf("供应商任务查询超时，请联系管理员核对上游任务")
			}
			if p["protocol"] == "openai-video" && result["url"] == nil && result["video_url"] == nil {
				upstreamPhase(ctx, "receiving")
				res, err := client.bytes("/videos/"+url.PathEscape(str(upID))+"/content", nil, "")
				if err != nil {
					return nil, err
				}
				data, err := boundedBody(res, 100*1024*1024)
				if err != nil {
					return nil, err
				}
				mt, ext := detectMime(data)
				if !strings.HasPrefix(mt, "video/") {
					return nil, fmt.Errorf("供应商返回的视频类型不受支持")
				}
				out, err := a.saveAsset(str(j["user_id"]), str(j["id"]), "", mt, ext, data)
				return []Row{out}, err
			}
		}
	}
	var configured any
	if config["outputPath"] != nil {
		configured = pathValue(result, str(config["outputPath"]))
	}
	raw := first(configured, pathValue(result, "data.result_urls"), result["result_urls"], pathValue(result, "data.url"), result["video_url"], result["url"], result["data"], result["output"])
	outputs, ok := raw.([]any)
	if !ok && raw != nil {
		outputs = []any{raw}
	}
	if len(outputs) == 0 {
		return nil, fmt.Errorf("供应商未返回生成结果")
	}
	return a.saveProviderOutputs(ctx, j, outputs, "", "")
}
func (a *App) saveProviderOutputs(ctx context.Context, j Row, outputs []any, trustedBase, key string) ([]Row, error) {
	upstreamPhase(ctx, "receiving")
	var e error
	if len(outputs) == 0 {
		return nil, fmt.Errorf("供应商未返回生成结果")
	}
	saved := []Row{}
	limit := 10
	if snapshot := object(j["provider_snapshot"]); snapshot["type"] == "image" && snapshot["maxOutputsPerRequest"] != nil {
		limit = 1
		if integer(snapshot["maxOutputsPerRequest"]) == 4 {
			limit = 4
		}
	}
	for i, output := range outputs {
		if i >= limit {
			break
		}
		o := object(output)
		var data []byte
		var mt, ext string
		if o["b64_json"] != nil {
			encoded := str(o["b64_json"])
			if len(encoded) > 42*1024*1024 {
				return nil, fmt.Errorf("供应商生成图片过大")
			}
			data, e = base64.StdEncoding.DecodeString(encoded)
			if e != nil {
				return nil, fmt.Errorf("供应商图片编码无效")
			}
			mt, ext = detectMime(data)
			if !strings.HasPrefix(mt, "image/") {
				return nil, fmt.Errorf("供应商图片类型无效")
			}
		} else {
			address := str(first(o["url"], o["video_url"], output))
			if strings.HasPrefix(address, "/") && trustedBase != "" {
				base, e := url.Parse(trustedBase)
				if e != nil {
					return nil, e
				}
				relative, e := url.Parse(address)
				if e != nil {
					return nil, e
				}
				address = base.ResolveReference(relative).String()
			}
			u, err := url.Parse(address)
			if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
				return nil, fmt.Errorf("供应商返回了不支持的文件地址")
			}
			data, e = downloadVendorOutput(ctx, address, trustedBase, key)
			if e != nil {
				return nil, e
			}
			mt, ext = detectMime(data)
			if mt == "" || strings.HasPrefix(mt, "audio/") {
				return nil, fmt.Errorf("供应商返回的文件类型不受支持")
			}
		}
		out, err := a.saveAsset(str(j["user_id"]), str(j["id"]), "", mt, ext, data)
		if err != nil {
			return nil, err
		}
		saved = append(saved, out)
	}
	return saved, nil
}
func downloadVendorOutput(ctx context.Context, address, trustedBase, key string) ([]byte, error) {
	return downloadVendorOutputAuth(ctx, address, trustedBase, key, "Authorization")
}
func downloadVendorOutputAuth(ctx context.Context, address, trustedBase, key, authHeader string) ([]byte, error) {
	trusted, _ := url.Parse(trustedBase)
	for redirects := 0; redirects <= 4; redirects++ {
		u, e := url.Parse(address)
		if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return nil, fmt.Errorf("供应商生成文件地址无效")
		}
		sameOrigin := trusted != nil && trusted.Host == u.Host && trusted.Scheme == u.Scheme
		client := outputClient()
		if sameOrigin {
			client = upstreamClient()
			client.Timeout = 90 * time.Second
		}
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		req, e := http.NewRequestWithContext(ctx, "GET", address, nil)
		if e != nil {
			return nil, e
		}
		if sameOrigin && key != "" {
			value := key
			if authHeader == "Authorization" {
				value = "Bearer " + key
			}
			req.Header.Set(authHeader, value)
		}
		res, e := client.Do(req)
		if e != nil {
			client.CloseIdleConnections()
			return nil, fmt.Errorf("供应商生成文件下载失败")
		}
		if res.StatusCode >= 300 && res.StatusCode < 400 {
			location := res.Header.Get("Location")
			res.Body.Close()
			client.CloseIdleConnections()
			next, e := url.Parse(location)
			if e != nil || location == "" {
				return nil, fmt.Errorf("供应商下载跳转地址无效")
			}
			address = u.ResolveReference(next).String()
			continue
		}
		data, e := boundedBody(res, 100*1024*1024)
		client.CloseIdleConnections()
		return data, e
	}
	return nil, fmt.Errorf("供应商生成文件跳转次数过多")
}

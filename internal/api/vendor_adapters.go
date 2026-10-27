package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func isVendor(protocol any) bool {
	switch str(protocol) {
	case "openvideo", "suda", "modelhub", "gway", "catcoder", "wxart", "rolldek", "maitoken", "opencrow":
		return true
	}
	return false
}

type vendorFile struct {
	field string
	asset Row
}

func (a *App) vendorForm(values Row, files []vendorFile) (*bytes.Buffer, string, error) {
	return a.vendorFormRepeated(values, files, nil)
}
func (a *App) vendorFormRepeated(values Row, files []vendorFile, repeated map[string][]string) (*bytes.Buffer, string, error) {
	body := new(bytes.Buffer)
	w := multipart.NewWriter(body)
	for name, value := range values {
		s := str(value)
		switch value.(type) {
		case map[string]any, []any, []string, []Row:
			s = jsonText(value)
		}
		if e := w.WriteField(name, s); e != nil {
			return nil, "", e
		}
	}
	for name, values := range repeated {
		for _, value := range values {
			if e := w.WriteField(name, value); e != nil {
				return nil, "", e
			}
		}
	}
	for _, file := range files {
		name := strings.NewReplacer("\"", "", "\r", "", "\n", "").Replace(str(file.asset["name"]))
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, file.field, name))
		h.Set("Content-Type", str(file.asset["mime"]))
		part, e := w.CreatePart(h)
		if e != nil {
			return nil, "", e
		}
		f, e := os.Open(a.filePath(str(file.asset["file"])))
		if e != nil {
			return nil, "", e
		}
		_, e = io.Copy(part, io.LimitReader(f, 30*1024*1024+1))
		f.Close()
		if e != nil {
			return nil, "", e
		}
	}
	if e := w.Close(); e != nil {
		return nil, "", e
	}
	return body, w.FormDataContentType(), nil
}
func vendorHTTP(ctx context.Context, base, key, path string, body io.Reader, contentType, idem string) (Row, error) {
	return vendorHTTPBounded(ctx, base, key, path, body, contentType, idem, 16*1024*1024)
}
func vendorHTTPBounded(ctx context.Context, base, key, path string, body io.Reader, contentType, idem string, limit int64) (Row, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "..") || strings.Contains(path, "\\") {
		return nil, fmt.Errorf("供应商接口路径无效")
	}
	method := "GET"
	if body != nil {
		method = "POST"
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, body)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if idem != "" && method == "POST" {
		req.Header.Set("Idempotency-Key", idem)
	}
	if method == "POST" {
		req.GetBody = nil
	}
	client := upstreamClient()
	client.Timeout = 10 * time.Minute
	res, e := client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("供应商连接失败，请根据任务 ID 核对上游状态")
	}
	b, e := boundedBody(res, limit)
	if e != nil {
		return nil, e
	}
	var value Row
	if json.Unmarshal(b, &value) != nil {
		return nil, fmt.Errorf("供应商返回的数据格式不正确")
	}
	if value["success"] == false || num(value["code"]) < 0 {
		return nil, fmt.Errorf("供应商拒绝了请求，请核对模型、参数及可用额度")
	}
	return value, nil
}
func vendorValue(value any, path string) any {
	for _, part := range strings.Split(path, ".") {
		switch current := value.(type) {
		case map[string]any:
			value = current[part]
		case []any:
			i, e := strconv.Atoi(part)
			if e != nil || i < 0 || i >= len(current) {
				return nil
			}
			value = current[i]
		default:
			return nil
		}
	}
	return value
}
func vendorState(vendor string, value Row) string {
	if vendor == "suda" {
		return strings.ToLower(str(first(vendorValue(value, "data.status"), vendorValue(value, "data.data.state"), value["status"])))
	}
	return strings.ToLower(str(first(value["status"], vendorValue(value, "data.status"), value["state"])))
}
func vendorResult(vendor string, value Row) []any {
	var result any
	switch vendor {
	case "openvideo":
		result = first(value["source_urls"], value["result_urls"])
		if len(array(result)) == 0 {
			result = value["result_urls"]
		}
	case "suda":
		result = first(vendorValue(value, "data.result_url"), vendorValue(value, "data.data.creations"))
	case "catcoder":
		result = first(value["download_url"], value["result_urls"], value["url"], value["data"])
	case "modelhub":
		result = first(vendorValue(value, "result.url"), value["data"], value["result_urls"], value["url"])
	default:
		result = first(value["result_urls"], value["video_url"], vendorValue(value, "content.video_url"), vendorValue(value, "metadata.url"), value["data"], value["url"])
	}
	if result == nil {
		return nil
	}
	if list, ok := result.([]any); ok {
		return list
	}
	return []any{result}
}

var referencePattern = regexp.MustCompile(`(?:参考|@)(图片|视频|音频)([0-9]+)`)

func vendorPrompt(vendor, prompt, kind string) string {
	return referencePattern.ReplaceAllStringFunc(prompt, func(token string) string {
		parts := referencePattern.FindStringSubmatch(token)
		t := map[string]string{"图片": "image", "视频": "video", "音频": "audio"}[parts[1]]
		switch vendor {
		case "openvideo", "modelhub":
			return "@" + t + "_file_" + parts[2]
		case "suda":
			if kind == "image" && t == "image" {
				return "图" + parts[2]
			}
			return "@" + t + parts[2]
		case "gway", "wxart":
			return "@" + parts[1] + parts[2]
		default:
			return token
		}
	})
}
func (a *App) vendorUpload(ctx context.Context, provider Row, key string, asset Row) (string, error) {
	base, path := str(provider["base_url"]), "/media/upload"
	switch provider["protocol"] {
	case "suda":
		base = fallback(object(provider["config"])["uploadBaseURL"], "https://files.sudashuiapi.com")
		path = "/"
		if asset["type"] == "audio" && integer(asset["size"]) > 15*1024*1024 {
			return "", fmt.Errorf("供应商 Suda 音频素材不能超过 15MB")
		}
	case "media-upload":
		path = fallback(object(provider["config"])["uploadPath"], "/files")
	}
	fields := Row{}
	if provider["protocol"] == "gway" {
		fields["kind"] = asset["type"]
	}
	body, ct, e := a.vendorForm(fields, []vendorFile{{"file", asset}})
	if e != nil {
		return "", e
	}
	result, e := vendorHTTP(ctx, base, key, path, body, ct, "")
	if e != nil {
		return "", e
	}
	address := str(first(result["url"], vendorValue(result, "data.fileUrl"), vendorValue(result, "data.url"), vendorValue(result, "data.0.url"), vendorValue(result, "data.items.0.url")))
	u, e := url.Parse(address)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("供应商上传接口未返回可用的 HTTPS 素材链接")
	}
	return address, nil
}
func (a *App) hostedReferences(ctx context.Context, provider Row, key string, refs []Row) (map[string][]string, error) {
	result := map[string][]string{"image": {}, "video": {}, "audio": {}}
	if len(refs) == 0 {
		return result, nil
	}
	storage, storageErr := a.storageConfig(a.Store)
	if storageErr != nil {
		return nil, storageErr
	}
	if storage.Enabled && provider["protocol"] != "suda" && provider["protocol"] != "gway" {
		for _, ref := range refs {
			asset, err := a.ensureOSSAsset(ctx, storage, ref)
			if err != nil {
				return nil, err
			}
			remote := object(asset["remote_storage"])
			address, err := a.ossURL(remoteOSSConfig(storage, remote), str(remote["objectKey"]), false)
			if err != nil {
				return nil, err
			}
			kind := str(ref["type"])
			result[kind] = append(result[kind], address)
		}
		return result, nil
	}
	uploader := provider
	uploadKey := key
	if provider["protocol"] != "suda" && provider["protocol"] != "gway" {
		uploadID := str(object(provider["config"])["uploadProviderId"])
		if uploadID != "" {
			uploader = a.Store.One("SELECT * FROM providers WHERE id=? AND protocol='media-upload' AND enabled=1", uploadID)
		} else {
			uploader = a.Store.One("SELECT * FROM providers WHERE protocol='media-upload' AND enabled=1 ORDER BY created_at LIMIT 1")
		}
		if uploader == nil {
			return nil, fmt.Errorf("供应商此接口需要公网素材，请先配置公共素材上传服务")
		}
		var e error
		uploadKey, e = a.decrypt(str(uploader["api_key"]))
		if e != nil || uploadKey == "" {
			return nil, fmt.Errorf("供应商公共素材上传服务密钥不可用")
		}
	}
	for _, ref := range refs {
		address, e := a.vendorUpload(ctx, uploader, uploadKey, ref)
		if e != nil {
			return nil, e
		}
		kind := str(ref["type"])
		result[kind] = append(result[kind], address)
	}
	return result, nil
}
func copyVendorFields(target, params Row, names ...string) {
	for _, name := range names {
		if value, ok := params[name]; ok && value != nil && value != "" {
			target[name] = value
		}
	}
}
func (a *App) vendorUpstream(ctx context.Context, j, p Row, key string, refs []Row) ([]Row, error) {
	vendor := str(p["protocol"])
	if vendor == "rolldek" {
		return a.rolldekUpstream(ctx, j, p, key, refs)
	}
	if vendor == "maitoken" {
		return a.maiTokenUpstream(ctx, j, p, key, refs)
	}
	params := object(j["params"])
	kind := str(p["type"])
	base := str(p["base_url"])
	config := object(p["config"])
	prompt := vendorPrompt(vendor, str(j["prompt"]), kind)
	body := Row{"model": p["upstreamModel"], "prompt": prompt}
	path, pollPath := "", ""
	synchronous := false
	files := []vendorFile{}
	ratio := first(params["aspectRatio"], params["aspect_ratio"], params["ratio"])
	mode := str(first(params["function_mode"], params["functionMode"], params["mode"]))
	switch vendor {
	case "openvideo", "modelhub":
		copyVendorFields(body, params, "resolution", "duration", "quality", "detail_level", "generate_audio", "seed", "negative_prompt", "channel")
		if ratio != nil {
			body["ratio"] = ratio
		}
		path = "/videos/generations"
		pollPath = "/videos/tasks/{id}"
		if vendor == "openvideo" {
			body["face"] = Row{"enabled": truth(params["faceEnabled"])}
			if truth(params["faceEnabled"]) {
				body["face"] = Row{"enabled": true, "mode": "light"}
			}
			pollPath = "/tasks/{id}"
			if kind == "video" && body["resolution"] != nil {
				body["video_resolution"] = body["resolution"]
				delete(body, "resolution")
			}
		}
		if kind == "image" {
			path = "/images/generations"
			if vendor == "modelhub" {
				synchronous = true
				body["image_count"] = first(params["image_count"], params["n"], 1)
				if len(refs) > 0 {
					path = "/images/compositions"
				}
			}
			for _, ref := range refs {
				if ref["type"] != "image" {
					return nil, fmt.Errorf("供应商图片模型仅接受参考图片")
				}
				field := "images"
				files = append(files, vendorFile{field, ref})
			}
		} else {
			if mode == "" {
				mode = "omni_reference"
			}
			modeKey := "functionMode"
			if vendor == "openvideo" {
				modeKey = "function_mode"
			}
			body[modeKey] = mode
			counts := map[string]int{}
			for _, ref := range refs {
				t := str(ref["type"])
				counts[t]++
				field := t + "_file_" + str(counts[t])
				if vendor == "openvideo" && mode == "first_last_frames" {
					if t != "image" || counts[t] > 2 {
						return nil, fmt.Errorf("供应商首尾帧模式只接受两张图片")
					}
					field = "first_frame"
					if counts[t] == 2 {
						field = "end_frame"
					}
				}
				if vendor == "openvideo" && mode == "multi_frame" {
					if t != "image" {
						return nil, fmt.Errorf("供应商多帧模式只接受图片")
					}
					field = "frame_" + str(counts[t])
				}
				files = append(files, vendorFile{field, ref})
			}
		}
	case "suda":
		hosted, e := a.hostedReferences(ctx, p, key, refs)
		if e != nil {
			return nil, e
		}
		payload := Row{}
		path = "/video/generations"
		pollPath = "/video/generations/{id}"
		if kind == "image" {
			payload["ratio"] = first(ratio, "1:1")
			copyVendorFields(payload, params, "resolution")
			if len(hosted["video"])+len(hosted["audio"]) > 0 {
				return nil, fmt.Errorf("供应商图片模型仅支持参考图片")
			}
			if len(hosted["image"]) > 0 {
				payload["imageUrls"] = hosted["image"]
			}
		} else {
			body["duration"] = first(params["duration"], 5)
			payload["aspectRatio"] = first(ratio, "16:9")
			if mode == "frames" || mode == "first_last_frames" {
				if len(hosted["image"]) != 2 || len(hosted["video"])+len(hosted["audio"]) > 0 {
					return nil, fmt.Errorf("供应商首尾帧模式需要两张图片")
				}
				payload["mode"] = "frames"
				payload["firstFrameUrl"] = hosted["image"][0]
				payload["lastFrameUrl"] = hosted["image"][1]
			} else {
				payload["mode"] = "references"
				for t, name := range map[string]string{"image": "imageUrls", "video": "videoUrls", "audio": "audioUrls"} {
					if len(hosted[t]) > 0 {
						payload[name] = hosted[t]
					}
				}
			}
		}
		body["metadata"] = Row{"payload": jsonText(payload)}
	case "gway":
		hosted, e := a.hostedReferences(ctx, p, key, refs)
		if e != nil {
			return nil, e
		}
		copyVendorFields(body, params, "duration", "resolution", "quality", "n", "generate_audio")
		if ratio != nil {
			body["ratio"] = ratio
		}
		for t, name := range map[string]string{"image": "images", "video": "videos", "audio": "audios"} {
			if len(hosted[t]) > 0 {
				body[name] = hosted[t]
			}
		}
		path = "/video/generations"
		pollPath = "/videos/tasks/{id}"
		if kind == "image" {
			path = "/images/create"
			pollPath = "/images/tasks/{id}"
		}
	case "catcoder":
		copyVendorFields(body, params, "resolution", "quality", "duration", "generate_audio", "watermark", "return_last_frame", "web_search", "seed", "video_mode")
		if ratio != nil {
			body["aspect_ratio"] = ratio
		}
		path = "/videos/generations"
		pollPath = "/videos/{id}"
		if kind == "image" {
			path = "/images/generations"
			pollPath = "/images/{id}"
			body["n"] = 1
			var size int64
			for _, ref := range refs {
				if ref["type"] != "image" {
					return nil, fmt.Errorf("供应商图片模型仅接受图片")
				}
				size += integer(ref["size"])
				files = append(files, vendorFile{"image", ref})
			}
			if size > 8*1024*1024 {
				return nil, fmt.Errorf("供应商 Catcoder 图片参考文件合计不能超过 8MiB")
			}
			if len(refs) > 0 {
				path = "/images/edits"
			}
		} else {
			hosted, e := a.hostedReferences(ctx, p, key, refs)
			if e != nil {
				return nil, e
			}
			for t, name := range map[string]string{"image": "images", "video": "videos", "audio": "audio"} {
				if len(hosted[t]) > 0 {
					body[name] = hosted[t]
				}
			}
		}
	case "wxart":
		if kind == "image" {
			path = "/images/generations"
			synchronous = true
			if p["upstreamModel"] == "nano-banana2" || p["upstreamModel"] == "nano-banana-pro" {
				body["resolution"] = first(params["resolution"], "1k")
				body["aspect_ratio"] = first(ratio, "1:1")
				hosted, e := a.hostedReferences(ctx, p, key, refs)
				if e != nil {
					return nil, e
				}
				if len(hosted["video"])+len(hosted["audio"]) > 0 {
					return nil, fmt.Errorf("供应商图片模型仅接受参考图片")
				}
				if len(hosted["image"]) > 0 {
					body["images_url"] = hosted["image"]
				}
				break
			}
			body["n"] = first(params["n"], 1)
			size := first(params["size"], "1024x1024")
			if ratio == "16:9" {
				size = "1536x1024"
			}
			if ratio == "9:16" {
				size = "1024x1536"
			}
			body["size"] = size
			copyVendorFields(body, params, "quality")
			for _, ref := range refs {
				if ref["type"] != "image" {
					return nil, fmt.Errorf("供应商图片模型仅接受图片")
				}
				files = append(files, vendorFile{"image[]", ref})
			}
			if len(files) > 0 {
				path = "/images/edits"
			}
		} else {
			path = "/videos"
			pollPath = "/videos/{id}"
			body["seconds"] = first(params["duration"], params["seconds"], 5)
			copyVendorFields(body, params, "resolution", "generate_audio", "watermark")
			if p["upstreamModel"] == "omni-flash" {
				copyVendorFields(body, params, "mode")
			}
			if ratio != nil {
				body["aspect_ratio"] = ratio
			}
			hosted, e := a.hostedReferences(ctx, p, key, refs)
			if e != nil {
				return nil, e
			}
			references := []Row{}
			for _, t := range []string{"image", "video", "audio"} {
				for _, address := range hosted[t] {
					references = append(references, Row{"type": t, "role": "reference_" + t, "source": address})
				}
			}
			if len(references) > 0 {
				body["references"] = references
			}
		}
	default:
		return nil, fmt.Errorf("供应商适配器不可用")
	}
	if kind == "image" {
		for _, name := range []string{"duration", "generate_audio", "watermark", "return_last_frame", "web_search", "video_mode", "functionMode", "function_mode"} {
			delete(body, name)
		}
	}
	var input io.Reader
	ct := "application/json"
	if len(files) > 0 || vendor == "openvideo" {
		form, contentType, e := a.vendorForm(body, files)
		if e != nil {
			return nil, e
		}
		input = form
		ct = contentType
	} else {
		input = strings.NewReader(jsonText(body))
	}
	result, e := vendorHTTP(ctx, base, key, path, input, ct, "pf-"+str(j["id"]))
	if e != nil {
		return nil, e
	}
	if synchronous {
		return a.saveProviderOutputs(ctx, j, vendorResult(vendor, result), base, key)
	}
	upID := str(first(result["task_id"], result["id"], vendorValue(result, "data.task_id"), vendorValue(result, "data.id")))
	if upID == "" || len(upID) > 255 {
		return nil, fmt.Errorf("供应商未返回有效任务 ID，请核对上游记录")
	}
	a.setUpstreamTaskID(j["id"], upID)
	interval := time.Duration(num(first(config["pollInterval"], 5000))) * time.Millisecond
	if interval < 20*time.Millisecond {
		interval = 20 * time.Millisecond
	}
	if vendor == "catcoder" && config["pollInterval"] == nil {
		interval = 10 * time.Second
	}
	for {
		state := vendorState(vendor, result)
		switch state {
		case "success", "succeeded", "completed", "done":
			return a.saveProviderOutputs(ctx, j, vendorResult(vendor, result), base, key)
		case "failure", "failed", "error", "cancelled", "canceled", "refunded", "expired":
			return nil, fmt.Errorf("供应商任务失败，请查看供应商控制台核对原因")
		}
		if e := wait(ctx, interval); e != nil {
			return nil, e
		}
		for attempt := 0; attempt < 3; attempt++ {
			result, e = vendorHTTP(ctx, base, key, strings.ReplaceAll(pollPath, "{id}", url.PathEscape(upID)), nil, "", "")
			if e == nil {
				break
			}
			if wait(ctx, interval) != nil {
				return nil, ctx.Err()
			}
		}
		if e != nil {
			return nil, e
		}
	}
}

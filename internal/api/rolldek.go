package api

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"
)

//go:embed rolldek_catalog.json
var rolldekCatalogJSON []byte
var rolldekCatalog = func() Row {
	var value Row
	must(json.Unmarshal(rolldekCatalogJSON, &value))
	return object(value["models"])
}()

func rolldekDefinition(model string) Row {
	value, ok := rolldekCatalog[model]
	if !ok {
		return nil
	}
	return object(value)
}

var rolldek25Sizes = map[string][]string{
	"3:1": {"1536x512", "3072x1024", "3840x1280"}, "21:9": {"1344x576", "2688x1152", "3840x1648"},
	"16:9": {"1280x720", "2048x1152", "3840x2160"}, "3:2": {"1152x768", "2304x1536", "3520x2352"},
	"4:3": {"1024x768", "2048x1536", "3312x2480"}, "1:1": {"1024x1024", "2048x2048", "2880x2880"},
	"3:4": {"768x1024", "1536x2048", "2480x3312"}, "2:3": {"768x1152", "1536x2304", "2352x3520"},
	"9:16": {"720x1280", "1152x2048", "2160x3840"}, "1:3": {"512x1536", "1024x3072", "1280x3840"},
}

func (a *App) rolldekInlineReference(asset Row) (Row, error) {
	if asset["type"] != "image" {
		return nil, fmt.Errorf("供应商此图片模型仅接受参考图片")
	}
	data, e := os.ReadFile(a.filePath(str(asset["file"])))
	if e != nil {
		return nil, e
	}
	if len(data) > 30*1024*1024 {
		return nil, fmt.Errorf("供应商参考图片超过本平台文件大小限制")
	}
	return Row{"mimeType": asset["mime"], "data": base64.StdEncoding.EncodeToString(data)}, nil
}
func rolldekPrompt(prompt string) string {
	return referencePattern.ReplaceAllStringFunc(prompt, func(token string) string {
		parts := referencePattern.FindStringSubmatch(token)
		return "@" + map[string]string{"图片": "Image", "视频": "Video", "音频": "Audio"}[parts[1]] + parts[2]
	})
}
func (a *App) rolldekUpstream(ctx context.Context, j, p Row, key string, refs []Row) ([]Row, error) {
	model := str(p["upstreamModel"])
	definition := rolldekDefinition(model)
	if definition == nil {
		return nil, fmt.Errorf("供应商 RollDek 此模型尚未配置经文档核对的接口")
	}
	base := strings.TrimSuffix(strings.TrimRight(str(p["base_url"]), "/"), "/v1")
	params := object(j["params"])
	adapter := str(definition["adapter"])
	ratio := first(params["aspectRatio"], params["ratio"], "16:9")
	body := Row{"model": model, "prompt": j["prompt"]}
	path := "/v1/images/generations"
	files := []vendorFile{}
	synchronous := definition["kind"] == "image"
	if synchronous {
		for _, ref := range refs {
			if ref["type"] != "image" {
				return nil, fmt.Errorf("供应商图片模型仅接受参考图片")
			}
		}
	}
	switch adapter {
	case "openai-images", "image25":
		if adapter == "image25" {
			resolution := strings.ToUpper(str(first(params["resolution"], "1K")))
			index, ok := map[string]int{"1K": 0, "2K": 1, "4K": 2}[resolution]
			sizes := rolldek25Sizes[str(first(params["aspectRatio"], "1:1"))]
			if !ok || len(sizes) != 3 {
				return nil, fmt.Errorf("供应商 GPT Image 2.5 的分辨率或比例不受支持")
			}
			if model == "gpt-image-2.5" && resolution != "1K" {
				return nil, fmt.Errorf("供应商 gpt-image-2.5 仅支持 1K")
			}
			body["size"] = sizes[index]
			if model != "gpt-image-2.5" {
				copyVendorFields(body, params, "quality")
			}
			// Non-official groups reject n, including n=1; one image is generated.
		} else {
			copyVendorFields(body, params, "size", "quality")
			body["n"] = first(params["n"], 1)
			body["response_format"] = "url"
			if model == "gpt-image-2" {
				body["quality"] = "medium"
			}
		}
		if len(refs) > 0 {
			path = "/v1/images/edits"
			for _, ref := range refs {
				if ref["mime"] != "image/png" && ref["mime"] != "image/jpeg" && ref["mime"] != "image/webp" {
					return nil, fmt.Errorf("供应商图片编辑支持 PNG、JPEG 和 WebP")
				}
				files = append(files, vendorFile{"image[]", ref})
			}
		}
	case "gemini":
		path = "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
		parts := []Row{{"text": j["prompt"]}}
		for _, ref := range refs {
			image, e := a.rolldekInlineReference(ref)
			if e != nil {
				return nil, e
			}
			parts = append(parts, Row{"inlineData": image})
		}
		imageConfig := Row{"imageSize": strings.ToUpper(str(first(params["resolution"], "1K")))}
		if params["aspectRatio"] != nil && params["aspectRatio"] != "auto" {
			imageConfig["aspectRatio"] = params["aspectRatio"]
		}
		generation := Row{"responseModalities": []string{"IMAGE"}, "responseFormat": Row{"image": imageConfig}}
		if params["thinkingLevel"] != nil {
			generation["thinkingConfig"] = Row{"thinkingLevel": params["thinkingLevel"]}
		}
		body = Row{"contents": []Row{{"role": "user", "parts": parts}}, "generationConfig": generation}
	case "seedream":
		body["resolution"] = strings.ToLower(str(first(params["resolution"], "1k")))
		body["size"] = first(params["aspectRatio"], "auto")
		body["n"] = first(params["n"], 1)
		images := []string{}
		for _, ref := range refs {
			image, e := a.rolldekInlineReference(ref)
			if e != nil {
				return nil, e
			}
			images = append(images, "data:"+str(image["mimeType"])+";base64,"+str(image["data"]))
		}
		if len(images) > 0 {
			body["images"] = images
		}
	default:
		path = "/v1/videos"
		body["prompt"] = rolldekPrompt(str(j["prompt"]))
		body["duration"] = first(params["duration"], 4)
		body["aspect_ratio"] = ratio
		copyVendorFields(body, params, "resolution", "seed")
		mode := str(params["function_mode"])
		frames := mode == "first_last_frames"
		if frames {
			max := 2
			min := 2
			if truth(definition["stable"]) || adapter == "ark" {
				min = 1
			}
			if len(refs) < min || len(refs) > max {
				return nil, fmt.Errorf("供应商首尾帧模式的图片数量不正确")
			}
			for _, ref := range refs {
				if ref["type"] != "image" {
					return nil, fmt.Errorf("供应商首尾帧模式不能混用音视频参考")
				}
			}
		}
		hosted, e := a.hostedReferences(ctx, p, key, refs)
		if e != nil {
			return nil, e
		}
		switch adapter {
		case "sd":
			if frames {
				firstField, lastField := "first_image", "last_image"
				if truth(definition["stable"]) {
					firstField, lastField = "first_frame_image", "last_frame_image"
				}
				body[firstField] = hosted["image"][0]
				if len(hosted["image"]) > 1 {
					body[lastField] = hosted["image"][1]
				}
			} else {
				for media, field := range map[string]string{"image": "image_refs", "video": "video_refs", "audio": "audio_refs"} {
					if len(hosted[media]) > 0 {
						body[field] = hosted[media]
					}
				}
			}
		case "wan":
			body["prompt"] = strings.NewReplacer("@Image", "图", "@Video", "视频", "@Audio", "音频").Replace(str(body["prompt"]))
			body["seconds"] = str(body["duration"])
			delete(body, "duration")
			body["resolution"] = strings.ToUpper(str(first(definition["fixedResolution"], params["resolution"], "720p")))
			for _, media := range []string{"image", "video", "audio"} {
				list := []Row{}
				for i, address := range hosted[media] {
					item := Row{"url": address}
					if frames && media == "image" {
						item["role"] = []string{"first_frame", "last_frame"}[i]
					}
					list = append(list, item)
				}
				if len(list) > 0 {
					body["reference_"+media+"s"] = list
				}
			}
		case "ark":
			path = "/api/v3/contents/generations/tasks"
			content := []Row{{"type": "text", "text": j["prompt"]}}
			for _, media := range []string{"image", "video", "audio"} {
				for i, address := range hosted[media] {
					role := "reference_" + media
					if frames && media == "image" {
						role = []string{"first_frame", "last_frame"}[i]
					}
					content = append(content, Row{"type": media + "_url", media + "_url": Row{"url": address}, "role": role})
				}
			}
			body["content"] = content
			body["ratio"] = ratio
			delete(body, "prompt")
			delete(body, "aspect_ratio")
			copyVendorFields(body, params, "generate_audio", "watermark")
		case "generic-video":
			for media, field := range map[string]string{"image": "images", "video": "videos", "audio": "audios"} {
				if len(hosted[media]) > 0 {
					body[field] = hosted[media]
				}
			}
			copyVendorFields(body, params, "generateAudio", "preprocess")
		default:
			return nil, fmt.Errorf("供应商 RollDek 模型接口类型尚未支持")
		}
	}
	var input io.Reader
	contentType := "application/json"
	if len(files) > 0 {
		form, ct, e := a.vendorForm(body, files)
		if e != nil {
			return nil, e
		}
		input = form
		contentType = ct
	} else {
		input = strings.NewReader(jsonText(body))
	}
	limit := int64(16 * 1024 * 1024)
	if synchronous {
		limit = 128 * 1024 * 1024
	}
	result, e := vendorHTTPBounded(ctx, base, key, path, input, contentType, "pf-"+str(j["id"]), limit)
	if e != nil {
		return nil, e
	}
	if result["error"] != nil {
		return nil, fmt.Errorf("供应商拒绝了 RollDek 生成请求，请核对模型分组和参数")
	}
	if synchronous {
		outputs := vendorResult("rolldek", result)
		if adapter == "gemini" {
			outputs = []any{}
			for _, candidate := range array(result["candidates"]) {
				for _, part := range array(object(object(candidate)["content"])["parts"]) {
					value := object(part)
					if truth(value["thought"]) {
						continue
					}
					image := object(first(value["inlineData"], value["inline_data"]))
					if str(image["data"]) != "" {
						outputs = append(outputs, Row{"b64_json": image["data"]})
					}
				}
			}
		}
		if n := integer(params["n"]); n > 1 && int64(len(outputs)) < n {
			return nil, fmt.Errorf("供应商返回图片数量少于请求数量，请核对上游任务")
		}
		return a.saveProviderOutputs(ctx, j, outputs, base, key)
	}
	taskID := str(first(result["task_id"], result["id"], vendorValue(result, "data.id")))
	if taskID == "" || len(taskID) > 255 {
		return nil, fmt.Errorf("供应商未返回有效任务 ID，请核对上游记录")
	}
	a.Store.Exec("UPDATE jobs SET upstream_id=? WHERE id=?", taskID, j["id"])
	poll := "/v1/videos/" + url.PathEscape(taskID)
	if adapter == "ark" {
		poll = "/api/v3/contents/generations/tasks/" + url.PathEscape(taskID)
	}
	interval := time.Duration(num(first(object(p["config"])["pollInterval"], 15000))) * time.Millisecond
	if interval < 20*time.Millisecond {
		interval = 20 * time.Millisecond
	}
	for {
		switch vendorState("rolldek", result) {
		case "completed", "succeeded", "success":
			outputs := vendorResult("rolldek", result)
			if len(outputs) == 0 {
				outputs = []any{"/v1/videos/" + url.PathEscape(taskID) + "/content"}
			}
			return a.saveProviderOutputs(ctx, j, outputs, base, key)
		case "failed", "failure", "error", "expired", "cancelled", "canceled", "refunded":
			return nil, fmt.Errorf("供应商 RollDek 任务失败，请核对上游错误信息")
		}
		if e = wait(ctx, interval); e != nil {
			return nil, e
		}
		for attempt := 0; attempt < 3; attempt++ {
			result, e = vendorHTTP(ctx, base, key, poll, nil, "", "")
			if e == nil {
				break
			}
			if waitErr := wait(ctx, interval); waitErr != nil {
				return nil, waitErr
			}
		}
		if e != nil {
			return nil, e
		}
	}
}

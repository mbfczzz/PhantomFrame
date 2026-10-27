package api

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Public model IDs/types from OpenCrow's catalog, fetched 2026-10-27.
// No credentials or upstream prices are embedded as retail prices.
//
//go:embed opencrow_models.json
var openCrowCatalogJSON []byte
var openCrowCatalog = func() map[string]Row {
	var rows []Row
	must(json.Unmarshal(openCrowCatalogJSON, &rows))
	out := map[string]Row{}
	for _, r := range rows {
		out[str(r["id"])] = r
	}
	return out
}()

func openCrowDefinition(model string) Row { return openCrowCatalog[model] }

func openCrowParameters(model string) []any {
	spec := openCrowDefinition(model)
	if spec == nil {
		return nil
	}
	profile := str(spec["profile"])
	selectP := func(name, label string, options ...string) Row {
		v := []any{}
		for _, s := range options {
			v = append(v, s)
		}
		return choice(name, label, v, options[0])
	}
	number := func(name, label string, def, min, max int) Row {
		return Row{"name": name, "label": label, "type": "number", "default": def, "min": min, "max": max, "integer": true}
	}
	text := func(name, label, def string) Row {
		return Row{"name": name, "label": label, "type": "text", "default": def}
	}
	ref := func(kind string, max int, required bool) Row {
		p := referenceParameter(kind, max)
		if required {
			p["min"], p["required"] = 1, true
		}
		return p
	}
	switch profile {
	case "chat":
		limit := 8192
		if n := integer(spec["maxOutputTokens"]); n > 0 && n < int64(limit) {
			limit = int(n)
		}
		return []any{number("maxOutputTokens", "最大输出 Token", min(2048, limit), 1, limit), text("systemPrompt", "系统提示词", "")}
	case "tts":
		voice := text("voice", "音色", "Cherry")
		voice["required"] = true
		if !strings.HasPrefix(model, "qwen3-tts-") {
			voice["default"] = ""
		}
		p := []any{voice}
		if strings.Contains(model, "instruct") {
			p = append(p, text("style", "语气与风格", ""))
		}
		return p
	case "gpt_image":
		return []any{selectP("size", "图片尺寸", "1024x1024", "1024x1536", "1536x1024"), selectP("quality", "质量", "auto", "low", "medium", "high"), ref("image", 1, false)}
	case "gemini_image":
		res := []string{"1k", "2k", "4k"}
		if strings.Contains(model, "2.5") {
			res = []string{"1k"}
		}
		return []any{selectP("resolution", "分辨率", res...), selectP("aspectRatio", "画面比例", "1:1", "16:9", "9:16", "4:3", "3:4"), ref("image", 3, false)}
	case "seedream":
		res := []string{"2k", "4k"}
		if strings.HasPrefix(model, "dola-") {
			res = []string{"2k", "1k"}
		}
		return []any{selectP("resolution", "分辨率", res...), ref("image", 1, false)}
	case "grok_image":
		return []any{selectP("size", "图片尺寸", "1024x1024"), ref("image", 1, false)}
	case "qwen_image":
		if model == "qwen-image" || model == "qwen-image-plus" {
			return []any{selectP("size", "图片尺寸", "1024x1024", "1280x720", "720x1280")}
		}
		return []any{selectP("size", "图片尺寸", "1024x1024", "1280x720", "720x1280"), ref("image", 1, strings.Contains(model, "edit"))}
	case "motion":
		return []any{selectP("resolution", "分辨率", "720p", "1080p"), selectP("characterOrientation", "人物朝向", "image", "video"), ref("image", 1, true), ref("video", 1, true)}
	}
	res := []string{"720p", "1080p"}
	duration := number("duration", "生成时长（秒）", 5, 5, 10)
	if profile == "wan3" {
		res = []string{"480p", "720p", "1080p"}
		duration = number("duration", "生成时长（秒）", 5, 2, 30)
	}
	if profile == "seedance" {
		duration = number("duration", "生成时长（秒）", 5, 4, 10)
	}
	if profile == "grok_video" {
		res = []string{"720p", "480p"}
	}
	if profile == "minimax" {
		res = []string{"768P", "2K"}
		duration = number("duration", "生成时长（秒）", 5, 4, 15)
		if strings.HasSuffix(model, "-max") {
			res = []string{"768P", "480P"}
			duration["min"] = 5
		}
	}
	if profile == "kling" {
		res = []string{"720p", "1080p", "4k"}
		if strings.HasSuffix(model, "-turbo") {
			res = []string{"720p", "1080p"}
		}
		duration = number("duration", "生成时长（秒）", 5, 3, 15)
	}
	params := []any{selectP("resolution", "分辨率", res...), selectP("aspectRatio", "画面比例", "16:9", "9:16", "1:1"), duration}
	if profile == "wan" {
		params[2] = choice("duration", "生成时长（秒）", []any{float64(5), float64(10)}, 5)
	}
	if strings.Contains(model, "video-edit") {
		return []any{selectP("resolution", "分辨率", "720p", "1080p"), duration, ref("video", 1, true)}
	}
	if strings.Contains(model, "-t2v") {
		return params
	}
	if strings.Contains(model, "-i2v") {
		params[1] = selectP("aspectRatio", "画面比例", "16:9")
		return append(params, ref("image", 1, true))
	}
	if strings.Contains(model, "-r2v") {
		if profile == "happyhorse" {
			return append(params, ref("image", 3, true))
		}
		return append(params, ref("video", 1, true))
	}
	images := ref("image", 1, false)
	if profile == "wan3" {
		return append(params, ref("image", 3, false), ref("video", 1, false), ref("audio", 1, false))
	}
	if profile == "seedance" || profile == "minimax" {
		images = ref("image", 2, false)
		images["label"] = "首帧 / 尾帧图片（按顺序）"
	}
	return append(params, images)
}

func (a *App) openCrowProvider(s Store, m, p Row) Row {
	model := str(m["upstream_model"])
	spec := openCrowDefinition(model)
	if spec == nil || spec["kind"] != m["type"] {
		fail(400, "OpenCrow 模型或类型未通过适配，请重新同步模型目录")
	}
	p = clone(p)
	cfg := clone(object(p["config"]))
	cfg["vendor"], cfg["sourceModel"], cfg["upstreamModel"] = "opencrow", model, model
	p["config"] = cfg
	if entry := s.One("SELECT api_key FROM provider_models WHERE provider_id=? AND upstream_model=?", m["provider_id"], model); entry != nil && str(entry["api_key"]) != "" {
		p["api_key"] = entry["api_key"]
	}
	key, err := a.decrypt(str(p["api_key"]))
	if err != nil || !inventoryCredentialReady(key) {
		fail(400, "请先配置 OpenCrow API Key")
	}
	return p
}

func openCrowPlan(j, p Row, refs map[string][]string, files []Row) (inventoryCall, string, string, error) {
	cfg, params := object(p["config"]), object(j["params"])
	model := str(cfg["upstreamModel"])
	spec := openCrowDefinition(model)
	call := inventoryCall{Path: "/v1/images/generations"}
	if spec == nil {
		return call, "", "", fmt.Errorf("供应商 OpenCrow 模型未适配")
	}
	profile, kind := str(spec["profile"]), str(spec["kind"])
	prompt := str(j["prompt"])
	if (profile == "kling" || profile == "motion") && utf8.RuneCountInString(prompt) > 2500 {
		return call, "", "", fmt.Errorf("供应商此模型提示词最多 2500 个字符")
	}
	images, videos, audios := refs["image"], refs["video"], refs["audio"]
	for _, raw := range openCrowParameters(model) {
		field := object(raw)
		if field["type"] == "assets" {
			n := len(refs[str(field["itemType"])])
			if profile == "gpt_image" {
				n = len(files)
			}
			if truth(field["required"]) && n == 0 {
				return call, "", "", fmt.Errorf("供应商此模型需要%s", field["label"])
			}
			if field["max"] != nil && n > int(integer(field["max"])) {
				return call, "", "", fmt.Errorf("供应商参考素材数量超限")
			}
		}
	}
	resolution, ratio := str(params["resolution"]), str(params["aspectRatio"])
	duration := integer(params["duration"])
	body := Row{"model": model, "prompt": prompt}
	call.Body = body
	if kind == "text" {
		messages := []Row{}
		if system := str(params["systemPrompt"]); system != "" {
			messages = append(messages, Row{"role": "system", "content": system})
		}
		messages = append(messages, Row{"role": "user", "content": prompt})
		call.Path = "/v1/chat/completions"
		call.Body = Row{"model": model, "messages": messages, "stream": false}
		tokens := integer(params["maxOutputTokens"])
		if tokens < 1 || tokens > 8192 {
			return call, "", "", fmt.Errorf("供应商文本输出上限不正确")
		}
		tokenField := "max_tokens"
		if strings.HasPrefix(model, "gpt-5") || strings.HasPrefix(model, "gpt-6") || strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4") {
			tokenField = "max_completion_tokens"
		}
		object(call.Body)[tokenField] = tokens
		return call, "", "", nil
	}
	if kind == "audio" {
		call.Path = "/v1/audio/speech"
		call.Body = Row{"model": model, "input": prompt, "voice": params["voice"], "response_format": "mp3"}
		if style := str(params["style"]); style != "" {
			object(call.Body)["instructions"] = style
		}
		return call, "", "", nil
	}
	if kind == "image" {
		switch profile {
		case "gpt_image":
			body["n"], body["size"], body["quality"], body["output_format"] = 1, params["size"], params["quality"], "png"
			if len(files) > 0 {
				call.Path = "/v1/images/edits"
				call.Form = body
				call.Body = nil
				for _, file := range files {
					call.Files = append(call.Files, vendorFile{"image", file})
				}
			}
		case "gemini_image":
			input := []Row{{"type": "text", "text": prompt}}
			for _, img := range images {
				parts := strings.SplitN(img, ",", 2)
				if len(parts) != 2 {
					return call, "", "", fmt.Errorf("供应商 Gemini 参考图需要内嵌图片")
				}
				input = append(input, Row{"type": "image", "mime_type": strings.TrimSuffix(strings.TrimPrefix(parts[0], "data:"), ";base64"), "data": parts[1]})
			}
			call.Body = Row{"model": model, "input": input, "response_format": Row{"type": "image", "mime_type": "image/png", "image_size": strings.ToUpper(resolution), "aspect_ratio": ratio}, "store": false}
		case "qwen_image":
			content := []Row{{"text": prompt}}
			for _, img := range images {
				content = append(content, Row{"image": img})
			}
			call.Body = Row{"model": model, "input": Row{"messages": []Row{{"role": "user", "content": content}}}, "parameters": Row{"size": strings.ReplaceAll(str(params["size"]), "x", "*"), "n": 1, "watermark": false, "prompt_extend": false}}
			if model == "qwen-image" || model == "qwen-image-plus" {
				object(call.Body)["input"] = Row{"prompt": prompt}
			}
		case "seedream":
			body["size"], body["watermark"], body["sequential_image_generation"], body["response_format"] = strings.ToUpper(resolution), false, "disabled", "url"
			if len(images) > 0 {
				body["image"] = images
			}
		case "grok_image":
			body["size"], body["n"], body["response_format"] = params["size"], 1, "url"
			if len(images) > 0 {
				body["image"] = Row{"url": images[0]}
			}
		}
		return call, "", "", nil
	}
	call.Path = "/v1/videos/generations"
	body["nonce"], body["wait"] = str(j["id"]), false
	switch profile {
	case "seedance", "minimax":
		content := []Row{{"type": "text", "text": prompt}}
		for i, img := range images {
			role := "first_frame"
			if i > 0 {
				role = "last_frame"
			}
			content = append(content, Row{"type": "image_url", "image_url": Row{"url": img, "role": role}})
			if profile == "minimax" {
				content[len(content)-1] = Row{"type": "image_url", "image_url": Row{"url": img}, "role": role}
			}
		}
		body["content"], body["resolution"], body["ratio"], body["duration"] = content, resolution, ratio, duration
		if profile == "seedance" {
			body["generate_audio"], body["watermark"] = false, false
		} else {
			body["aigc_watermark"] = false
		}
	case "grok_video":
		body["duration"], body["resolution"], body["aspect_ratio"] = duration, resolution, ratio
		if len(images) > 0 {
			body["image"] = Row{"url": images[0]}
		}
	case "kling", "motion":
		settings := Row{"resolution": resolution, "audio": "off"}
		if strings.HasSuffix(model, "-turbo") {
			delete(settings, "audio")
		}
		body["settings"] = settings
		if profile == "motion" {
			settings["character_orientation"] = params["characterOrientation"]
			body["contents"] = []Row{{"type": "prompt", "text": prompt}, {"type": "image", "url": images[0]}, {"type": "video", "url": videos[0]}}
			delete(body, "prompt")
		} else {
			settings["duration"] = duration
			if len(images) > 0 {
				if ratio != "16:9" {
					return call, "", "", fmt.Errorf("供应商 Kling 图生视频跟随输入图片比例，请保持默认画面比例")
				}
				body["contents"] = []Row{{"type": "prompt", "text": prompt}, {"type": "image", "url": images[0]}}
				delete(body, "prompt")
			} else {
				settings["aspect_ratio"] = ratio
				if strings.HasSuffix(model, "-omni") {
					body["contents"] = []Row{{"type": "prompt", "text": prompt}}
					delete(body, "prompt")
				}
			}
		}
	default:
		input := Row{"prompt": prompt}
		parameters := Row{"resolution": strings.ToUpper(resolution), "duration": duration, "ratio": ratio, "watermark": false}
		body["input"], body["parameters"] = input, parameters
		delete(body, "prompt")
		if profile == "wan3" || strings.HasPrefix(model, "wan2.7") || profile == "happyhorse" {
			media := []Row{}
			imageRole := "reference_image"
			if strings.Contains(model, "-i2v") {
				imageRole = "first_frame"
			}
			for _, img := range images {
				media = append(media, Row{"type": imageRole, "url": img})
			}
			for _, video := range videos {
				media = append(media, Row{"type": "reference_video", "url": video})
			}
			for _, audio := range audios {
				media = append(media, Row{"type": "reference_audio", "url": audio})
			}
			if len(media) > 0 {
				input["media"] = media
			}
			if strings.HasPrefix(model, "wan2.7") && strings.Contains(model, "-i2v") {
				delete(input, "media")
				input["img_url"] = images[0]
			}
			if strings.Contains(model, "video-edit") {
				delete(input, "media")
				input["video_url"] = videos[0]
				w, h := inventoryDimensions(resolution, "16:9")
				parameters["size"] = fmt.Sprintf("%d*%d", w, h)
				delete(parameters, "ratio")
			}
		} else {
			w, h := inventoryDimensions(resolution, ratio)
			parameters["size"] = fmt.Sprintf("%d*%d", w, h)
			delete(parameters, "resolution")
			delete(parameters, "ratio")
			if len(images) > 0 {
				input["img_url"] = images[0]
			}
			if len(videos) > 0 {
				input["reference_urls"] = videos
			}
		}
		if profile == "wan3" {
			parameters["audio"], parameters["prompt_extend"] = false, false
		} else {
			body["duration"], body["resolution"] = duration, resolution
			if ratio != "" {
				body["aspect_ratio"] = ratio
			}
		}
	}
	return call, "/v1/videos/generations/{id}", "GET", nil
}

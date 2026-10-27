package api

import (
	"net/url"
	"strings"
	"unicode"
)

func inventoryCredentialReady(key string) bool {
	if len(key) < 8 || strings.IndexFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	switch strings.ToLower(key) {
	case "placeholder", "changeme", "replace-me", "your-api-key", "<redacted>":
		return false
	}
	return true
}

func inventoryVendorKnown(vendor string) bool {
	for _, name := range strings.Fields("afrifex ali apiyi atlascloud bytedance ccgo closeai copp dimensio dlapi dsas huanwangai infotoken kinovi ld midjourney-api mikotopro mxapi myapi mynewapi official-openai openrouter rolldek runninghub runninghub-upscale runware secure-skill secure-skill-out secure-skill-sepcial uniart wavespeed wxart zhubo zzshu") {
		if strings.EqualFold(name, vendor) {
			return true
		}
	}
	return false
}

func inventoryPathValid(path string) bool {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\\r\n#") || strings.Contains(path, "..") {
		return false
	}
	u, e := url.Parse(strings.ReplaceAll(path, "{id}", "task-id"))
	return e == nil && u.Host == "" && u.Scheme == "" && !strings.Contains(u.Path, "..")
}

func inventoryDefaultModel(vendor, model string, mappings Row) string {
	if mapped := str(mappings[model]); mapped != "" {
		return mapped
	}
	vendor = strings.ToLower(vendor)
	specific := map[string]map[string]string{
		"bytedance":  {"seedance-2-bytedance": "doubao-seedance-2-0-260128", "seedance-2-fast-bytedance": "doubao-seedance-2-0-fast-260128", "seedream-5.0-pro-bytedance": "doubao-seedream-5-0-pro-260628"},
		"afrifex":    {"seedance-2-afti": "seedance-2", "seedance-2-fast-afti": "seedance-2-fast", "seedance-2-ultra-afti": "seedance-2-pro-1080p", "video-fast-afti": "video-fast-720p", "video-fast-pro-afti": "video-pro-720p"},
		"runware":    {"gpt-image-2-official": "openai:gpt-image@2", "gpt-image-2.5": "openai:gpt-image@2.5", "seedance-2-runware": "bytedance:seedance@2.0", "seedance-2-fast-runware": "bytedance:seedance@2.0-fast", "kling-video-3-standard-runware": "klingai:kling-video@3-standard", "kling-video-3-pro-runware": "klingai:kling-video@3-pro", "kling-video-3-4k-runware": "klingai:kling-video@3-4k", "minimax-hailuo-02-runware": "minimax:3@1", "minimax-hailuo-2-3-runware": "minimax:4@1", "happyhorse-1-runware": "alibaba:happyhorse@1.0"},
		"openrouter": {"grok-imagine-image-quality": "x-ai/grok-imagine-image-quality", "grok-imagine-video": "x-ai/grok-imagine-video", "grok-imagine-video-1.5-preview": "x-ai/grok-imagine-video-1.5", "tts-gemini-openrouter": "google/gemini-3.1-flash-tts-preview", "tts-mai-openrouter": "microsoft/mai-voice-2", "tts-voxtral-openrouter": "mistralai/voxtral-mini-tts-2603", "lyria-3-openrouter": "google/lyria-3-pro-preview", "lyria-3-clip-openrouter": "google/lyria-3-clip-preview"},
		"mxapi":      {"suno-v4-mxapi": "chirp-v4", "suno-v4-5-mxapi": "chirp-bluejay", "suno-v5-mxapi": "chirp-crow", "suno-v5-5-mxapi": "chirp-fenix", "suno-sfx-mxapi": "chirp-crow"},
	}
	if value := specific[vendor][model]; value != "" {
		return value
	}
	if vendor == "copp" {
		return strings.Replace(model, "seedance-v2", "seedance-2", 1)
	}
	if vendor == "apiyi" && model == "gpt-image-2-vip-APIYI" {
		return "gpt-image-2-vip"
	}
	if vendor == "dimensio" {
		if model == "gpt_image-2-dimesio" {
			return "vxp-gpt-image-2"
		}
		if model == "gpt-image-2.5" {
			return "vxp-gpt-image-2.5"
		}
		return strings.TrimSuffix(model, "-Dimensio")
	}
	if strings.HasPrefix(model, "gpt-image-2") || strings.HasPrefix(model, "gpt_image-2") {
		name := "gpt-image-2"
		if strings.HasPrefix(model, "gpt-image-2.5") {
			name = "gpt-image-2.5"
		}
		if vendor == "openrouter" {
			name = "openai/" + name
		}
		return name
	}
	if strings.HasPrefix(model, "nano-banana-2") && vendor == "ccgo" {
		if strings.HasSuffix(model, "-ccgo") {
			return "gemini-3-pro-image"
		}
		return "gemini-3.1-flash-image"
	}
	return model
}

func inventoryParameters(r InventoryRoute) (result []any) {
	if strings.EqualFold(r.Vendor, "opencrow") {
		return openCrowParameters(r.Upstream)
	}
	defer func() {
		if strings.EqualFold(r.Vendor, "kinovi") {
			result = inventoryKinoviParams(r, result)
		}
		result = inventoryConstrainParameters(r, result)
	}()
	number := func(name, label string, def, min, max int) Row {
		return Row{"name": name, "label": label, "type": "number", "default": def, "min": min, "max": max, "integer": true}
	}
	text := func(name, label, def string) Row {
		return Row{"name": name, "label": label, "type": "text", "default": def}
	}
	selectField := func(name, label string, options []string, def string) Row {
		values := []any{}
		for _, s := range options {
			values = append(values, s)
		}
		return choice(name, label, values, def)
	}
	switch r.Modality {
	case "text":
		return []any{number("maxOutputTokens", "最大输出 Token", 2048, 1, 8192), text("systemPrompt", "系统提示词", "")}
	case "audio":
		if strings.Contains(strings.ToLower(r.Model), "tts") {
			voice := map[string]string{"tts-gemini-openrouter": "Zephyr", "tts-voxtral-openrouter": "neutral_female"}[r.Model]
			field := text("voice", "音色", voice)
			field["required"] = true
			return []any{field, text("style", "语气与风格", "")}
		}
		if strings.EqualFold(r.Vendor, "openrouter") {
			return []any{}
		}
		if strings.EqualFold(r.Vendor, "mxapi") && strings.Contains(r.Model, "sfx") {
			return []any{text("style", "音效风格", "")}
		}
		return []any{text("title", "作品标题", ""), text("style", "音乐风格", ""), Row{"name": "instrumental", "label": "纯音乐", "type": "boolean", "default": false}}
	case "upscale":
		ref := referenceParameter("video", 1)
		ref["min"] = 1
		ref["required"] = true
		return []any{ref, selectField("resolution", "目标分辨率", []string{"720p", "1080p", "2k", "4k"}, "1080p"), number("fps", "输出帧率", 30, 16, 60)}
	case "image":
		maxRefs := 9
		if strings.Contains(strings.ToLower(r.Vendor), "midjourney") || strings.EqualFold(r.Vendor, "atlascloud") {
			maxRefs = 1
		}
		return []any{selectField("aspectRatio", "画面比例", []string{"1:1", "16:9", "9:16", "4:3", "3:4"}, "1:1"), selectField("resolution", "分辨率", []string{"1k", "2k", "4k"}, "1k"), selectField("quality", "质量", []string{"auto", "low", "medium", "high"}, "auto"), referenceParameter("image", maxRefs)}
	default:
		params := []any{selectField("aspectRatio", "画面比例", []string{"16:9", "9:16", "1:1"}, "16:9"), selectField("resolution", "分辨率", []string{"720p", "1080p"}, "720p"), number("duration", "生成时长（秒）", 5, 4, 15), referenceParameter("image", 2)}
		return params
	}
}

func inventoryConstrainParameters(r InventoryRoute, params []any) []any {
	vendor := strings.ToLower(r.Vendor)
	result := []any{}
	for _, raw := range params {
		p := clone(object(raw))
		name := str(p["name"])
		if name == "resolution" {
			if vendor == "dsas" || vendor == "huanwangai" || vendor == "afrifex" && strings.HasPrefix(r.Model, "seedance-") {
				continue
			}
			if vendor == "kinovi" && strings.Contains(r.Model, "kling") && strings.Contains(r.Model, "4k") {
				p = choice(name, str(p["label"]), []any{"4k"}, "4k")
			}
			if vendor == "atlascloud" || vendor == "midjourney-api" && strings.Contains(r.Model, "midjourney") {
				p = choice(name, str(p["label"]), []any{"1k", "2k"}, "1k")
			}
		}
		if r.Modality == "image" && name == "quality" {
			if vendor == "ali" || vendor == "atlascloud" || vendor == "bytedance" || vendor == "huanwangai" || vendor == "midjourney-api" || (vendor == "apiyi" && r.Upstream == "gpt-image-2-vip") || ((vendor == "ccgo" || vendor == "mynewapi") && strings.HasPrefix(r.Model, "nano-banana")) {
				continue
			}
			if vendor == "kinovi" && inventoryKinoviSpec(r.Model, r.Upstream)["profile"] != "gpt" {
				continue
			}
			if vendor == "dimensio" || vendor == "infotoken" || vendor == "runninghub" {
				p = choice(name, "细节质量", []any{"low", "medium", "high"}, "medium")
			}
		}
		if r.Modality == "video" && name == "imageUrls" {
			switch vendor {
			case "bytedance", "infotoken", "uniart", "copp", "dimensio", "secure-skill-out":
				p["label"] = "首帧 / 尾帧图片（按顺序）"
			}
		}
		if vendor == "mikotopro" && r.Modality == "video" && name == "resolution" {
			p = choice(name, str(p["label"]), []any{"720p"}, "720p")
		}
		if vendor == "rolldek" && r.Modality == "video" && strings.HasSuffix(r.Upstream, "-720p") && name == "resolution" {
			p = choice(name, str(p["label"]), []any{"720p"}, "720p")
		}
		if r.Modality == "upscale" && name == "fps" {
			if vendor == "wavespeed" {
				continue
			}
			p = choice("fps", "输出帧率", []any{"1x", "2x", "3x", "4x", "16", "24", "25", "30", "48", "50", "60"}, "1x")
		}
		if vendor == "midjourney-api" && r.Modality == "image" && name == "resolution" && !strings.Contains(r.Model, "midjourney") {
			p = choice(name, str(p["label"]), []any{"1k", "4k"}, "1k")
		}
		if vendor == "runware" && r.Modality == "video" {
			if strings.HasSuffix(r.Upstream, "-standard") && name == "aspectRatio" {
				p = choice(name, str(p["label"]), []any{"16:9"}, "16:9")
			}
			if name == "imageUrls" {
				p["label"] = "首帧 / 尾帧图片（按顺序）"
				if strings.Contains(r.Model, "hailuo-2-3") || strings.Contains(r.Model, "happyhorse") {
					p["max"] = 1
				}
			}
			if strings.Contains(r.Model, "hailuo") {
				if name == "duration" {
					p = choice(name, str(p["label"]), []any{float64(6), float64(10)}, 6)
				}
				if name == "resolution" {
					p = choice(name, str(p["label"]), []any{"768p", "1080p"}, "768p")
				}
				if name == "aspectRatio" {
					p = choice(name, str(p["label"]), []any{"16:9"}, "16:9")
				}
			}
			if name == "resolution" && strings.Contains(r.Model, "kling-video-3") {
				resolution := "720p"
				if strings.Contains(r.Model, "pro") {
					resolution = "1080p"
				}
				if strings.Contains(r.Model, "4k") {
					resolution = "4k"
				}
				p = choice(name, str(p["label"]), []any{resolution}, resolution)
			}
			if name == "resolution" && strings.Contains(r.Model, "seedance-2-fast") {
				p = choice(name, str(p["label"]), []any{"480p", "720p"}, "720p")
			}
		}
		if vendor == "afrifex" && strings.HasPrefix(r.Model, "video-fast") && name == "resolution" {
			p = choice(name, str(p["label"]), []any{"480p", "720p"}, "720p")
		}
		result = append(result, p)
	}
	return result
}

func (a *App) validateInventoryQuote(s Store, m, p, params Row, uid string, prompts ...string) {
	refs := map[string][]string{"image": {}, "video": {}, "audio": {}}
	files := []Row{}
	var total int64
	nativeAssets := map[string]string{}
	if p["protocol"] == "opencrow" {
		for _, raw := range openCrowParameters(str(m["upstream_model"])) {
			field := object(raw)
			if field["type"] == "assets" {
				nativeAssets[str(field["name"])] = str(field["itemType"])
			}
		}
	}
	for _, raw := range array(m["parameters"]) {
		field := object(raw)
		if field["type"] != "assets" {
			continue
		}
		for _, assetID := range array(params[str(field["name"])]) {
			asset := s.One("SELECT * FROM assets WHERE id=? AND user_id=?", assetID, uid)
			if asset == nil {
				fail(400, "参考素材已不存在")
			}
			if p["protocol"] == "opencrow" && nativeAssets[str(field["name"])] != str(asset["type"]) {
				fail(400, "OpenCrow 参考素材类型与模型协议不一致，请修正模型参数，未扣费")
			}
			total += integer(asset["size"])
			refs[str(asset["type"])] = append(refs[str(asset["type"])], "https://reference.invalid/"+str(assetID))
			files = append(files, asset)
			if p["protocol"] == "opencrow" && openCrowDefinition(str(m["upstream_model"]))["profile"] == "motion" {
				a.validateOpenCrowMotionAsset(asset, params)
			}
		}
	}
	if total > 30*1024*1024 {
		fail(400, "此渠道一次提交的参考素材合计不能超过 30MB")
	}
	p = clone(p)
	p["type"] = m["type"]
	p["upstreamModel"] = m["upstream_model"]
	if p["protocol"] == "opencrow" && openCrowDefinition(str(m["upstream_model"]))["profile"] == "gemini_image" {
		for i := range refs["image"] {
			refs["image"][i] = "data:image/png;base64,cHJlZmxpZ2h0"
		}
	}
	prompt := "preflight"
	if len(prompts) > 0 {
		prompt = prompts[0]
	}
	_, _, _, err := inventoryPlan(Row{"id": "preflight", "params": params, "prompt": prompt}, p, refs, files)
	if err != nil {
		fail(400, err.Error())
	}
}

func (a *App) inventoryProvider(s Store, model, provider Row) Row {
	routeID := str(model["inventory_route_id"])
	route := s.One("SELECT * FROM inventory_routes WHERE id=?", routeID)
	if route == nil || route["provider_id"] != model["provider_id"] || route["modality"] != model["type"] || route["upstream_model"] != model["upstream_model"] || !truth(route["source_enabled"]) {
		fail(400, "供应商路由未启用或绑定已变更")
	}
	key, e := a.decrypt(str(route["api_key"]))
	if e != nil || !inventoryCredentialReady(key) || str(route["base_url"]) == "" {
		fail(400, "供应商路由尚未配置有效地址与密钥")
	}
	provider = clone(provider)
	provider["base_url"] = route["base_url"]
	provider["api_key"] = route["api_key"]
	config := object(route["config"])
	config["routeId"] = routeID
	config["vendor"] = route["vendor"]
	config["sourceModel"] = route["model"]
	config["upstreamModel"] = route["upstream_model"]
	provider["config"] = config
	return provider
}

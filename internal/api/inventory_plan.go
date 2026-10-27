package api

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var inventoryMJReserved = regexp.MustCompile(`(?i)(?:^|\s)--(?:v|version|hd|sd|q|quality|ar|aspect|niji|video|turbo)(?:\s|$)`)

func inventoryDimensions(resolution, ratio string) (int, int) {
	// Match the source adapters' image tiers. Video pixel resolutions retain
	// their own geometry below instead of treating 1K as a 1024-pixel long edge.
	tier := strings.ToLower(resolution)
	if tier == "1k" || tier == "2k" || tier == "4k" {
		index := map[string]int{"1k": 0, "2k": 1, "4k": 2}[tier]
		dimensions := map[string][3][2]int{
			"1:1":  {{1024, 1024}, {2048, 2048}, {4096, 4096}},
			"16:9": {{1280, 720}, {2560, 1440}, {3840, 2160}},
			"9:16": {{720, 1280}, {1440, 2560}, {2160, 3840}},
			"4:3":  {{1024, 768}, {2048, 1536}, {3200, 2400}},
			"3:4":  {{768, 1024}, {1536, 2048}, {2400, 3200}},
			"3:2":  {{1152, 768}, {2304, 1536}, {3456, 2304}},
			"2:3":  {{768, 1152}, {1536, 2304}, {2304, 3456}},
			"21:9": {{1344, 576}, {2688, 1152}, {3840, 1648}},
		}
		if sizes, ok := dimensions[ratio]; ok {
			return sizes[index][0], sizes[index][1]
		}
		return dimensions["1:1"][index][0], dimensions["1:1"][index][1]
	}
	size := 1024
	switch strings.ToLower(resolution) {
	case "2k":
		size = 2048
	case "4k":
		size = 4096
	case "720p":
		size = 1280
	case "1080p":
		size = 1920
	}
	parts := strings.Split(ratio, ":")
	w, h := 1.0, 1.0
	if len(parts) == 2 {
		w, _ = strconv.ParseFloat(parts[0], 64)
		h, _ = strconv.ParseFloat(parts[1], 64)
	}
	if w <= 0 || h <= 0 {
		w, h = 1, 1
	}
	if w >= h {
		return size, int(math.Round(float64(size)*h/w/8)) * 8
	}
	return int(math.Round(float64(size)*w/h/8)) * 8, size
}

// Pure request compilation is shared by pre-debit validation and mock contract tests.
// Route credentials never enter the body; media inputs have already passed ownership checks.
func inventoryPlan(j, p Row, refs map[string][]string, files []Row) (inventoryCall, string, string, error) {
	if err := inventoryValidateNativeParameters(p, object(j["params"])); err != nil {
		return inventoryCall{}, "", "", err
	}
	if object(p["config"])["vendor"] == "opencrow" {
		return openCrowPlan(j, p, refs, files)
	}
	call, poll, method, err := inventoryPlanNative(j, p, refs, files)
	cfg := object(p["config"])
	if override := str(cfg["submit_path_override"]); override != "" {
		call.Path = override
	}
	if override := str(cfg["poll_path_override"]); override != "" {
		poll = override
	}
	if call.Path != "" && !inventoryPathValid(call.Path) || poll != "" && !inventoryPathValid(poll) {
		err = fmt.Errorf("供应商路由接口路径不正确")
	}
	return call, poll, method, err
}

func inventoryPlanNative(j, p Row, refs map[string][]string, files []Row) (inventoryCall, string, string, error) {
	cfg, params := object(p["config"]), object(j["params"])
	vendor, kind := strings.ToLower(str(cfg["vendor"])), str(p["type"])
	if kind == "" {
		kind = str(object(j["provider_snapshot"])["type"])
	}
	model, source := str(first(cfg["upstreamModel"], p["upstreamModel"])), str(cfg["sourceModel"])
	prompt := str(j["prompt"])
	ratio := fallback(params["aspectRatio"], "16:9")
	resolution := fallback(params["resolution"], "720p")
	quality := fallback(params["quality"], "auto")
	duration := integer(params["duration"])
	if duration == 0 {
		duration = 5
	}
	path, poll, method := inventoryPaths(vendor, kind, source)
	if vendor == "wavespeed" {
		path = "/api/v3/" + model
	}
	body := Row{"model": model, "prompt": prompt}
	call := inventoryCall{Path: path, Body: body}
	images, videos := refs["image"], refs["video"]
	putImages := func(key string) {
		if len(images) > 0 {
			body[key] = images
		}
	}
	if kind == "text" {
		messages := []Row{}
		if system := str(params["systemPrompt"]); system != "" {
			messages = append(messages, Row{"role": "system", "content": system})
		}
		messages = append(messages, Row{"role": "user", "content": prompt})
		maxTokens := integer(params["maxOutputTokens"])
		if maxTokens < 1 || maxTokens > 8192 {
			return call, "", "", fmt.Errorf("文本输出上限必须在 1–8192 Token 内")
		}
		call.Path = "/v1/chat/completions"
		call.Body = Row{"model": model, "messages": messages, "max_tokens": maxTokens, "stream": false}
		if cfg["chatAPI"] == "responses" {
			call.Path = "/v1/responses"
			call.Body = Row{"model": model, "input": messages, "max_output_tokens": maxTokens, "stream": false}
		}
		return call, "", "", nil
	}
	if kind == "upscale" {
		if len(videos) != 1 {
			return call, "", "", fmt.Errorf("视频超分需要一份本账户的视频素材")
		}
		if vendor == "wavespeed" {
			call.Body = Row{"video": videos[0], "target_resolution": resolution}
		}
		if vendor == "runninghub-upscale" {
			call.Body = Row{"fileUrl": videos[0], "model": "max", "resolution": resolution, "outputFps": fallback(params["fps"], "1x")}
		}
		return call, poll, method, nil
	}
	if kind == "audio" && vendor == "openrouter" {
		if strings.HasPrefix(source, "tts-") {
			call.Body = Row{"model": model, "input": prompt, "voice": str(params["voice"]), "response_format": "mp3"}
			if style := str(params["style"]); style != "" {
				object(call.Body)["instructions"] = style
			}
		} else {
			call.Body = Row{"model": model, "messages": []Row{{"role": "user", "content": prompt}}, "modalities": []string{"text", "audio"}, "audio": Row{"format": "mp3"}}
		}
		return call, "", "", nil
	}
	if vendor == "kinovi" {
		body, err := inventoryKinoviBody(source, model, prompt, params, refs)
		call.Body = body
		return call, poll, method, err
	}
	switch vendor {
	case "ali":
		parts := []Row{}
		for _, image := range images {
			parts = append(parts, Row{"image": image})
		}
		parts = append(parts, Row{"text": prompt})
		w, h := inventoryDimensions(resolution, ratio)
		call.Body = Row{"model": model, "input": Row{"messages": []Row{{"role": "user", "content": parts}}}, "parameters": Row{"size": fmt.Sprintf("%d*%d", w, h), "n": 1, "watermark": false, "prompt_extend": false}}
	case "runware":
		w, h := inventoryDimensions(resolution, ratio)
		if kind == "image" && ratio == "1:1" && resolution == "4k" {
			w, h = 2880, 2880
		}
		if kind == "video" {
			if ratio == "1:1" {
				w, h = 960, 960
				if resolution == "1080p" {
					w, h = 1440, 1440
				}
			}
			if strings.HasPrefix(model, "bytedance:") && resolution == "480p" {
				w, h = 864, 496
				if ratio == "9:16" {
					w, h = 496, 864
				} else if ratio == "1:1" {
					w, h = 640, 640
				}
			}
		}
		task := Row{"taskUUID": str(j["id"]), "model": model, "positivePrompt": prompt, "numberResults": 1, "outputType": "URL", "deliveryMethod": "async"}
		if kind == "image" {
			task["taskType"] = "imageInference"
			task["outputFormat"] = "PNG"
			task["providerSettings"] = Row{"openai": Row{"quality": quality}}
			task["width"] = w
			task["height"] = h
			if len(images) > 0 {
				task["inputs"] = Row{"referenceImages": images}
			}
		} else {
			task["taskType"] = "videoInference"
			task["duration"] = duration
			if len(images) > 0 {
				frames := []Row{}
				for i, img := range images {
					position := "first"
					if i > 0 {
						position = "last"
					}
					frames = append(frames, Row{"image": img, "frame": position})
				}
				task["inputs"] = Row{"frameImages": frames}
			}
			if strings.HasPrefix(model, "bytedance:") && len(images) > 0 {
				task["resolution"] = resolution
			} else {
				task["width"] = w
				task["height"] = h
			}
			if strings.HasPrefix(model, "bytedance:") {
				task["audio"] = false
			}
			if strings.HasPrefix(model, "klingai:") {
				task["providerSettings"] = Row{"klingai": Row{"sound": false}}
				if len(images) > 0 || strings.HasSuffix(model, "-standard") {
					delete(task, "width")
					delete(task, "height")
				}
				if strings.HasSuffix(model, "-4k") && len(images) == 0 {
					vw, vh := 3840, 2160
					if ratio == "9:16" {
						vw, vh = 2160, 3840
					} else if ratio == "1:1" {
						vw, vh = 2880, 2880
					}
					task["width"], task["height"] = vw, vh
				}
			}
			if strings.HasPrefix(model, "minimax:") {
				if duration != 6 && duration != 10 {
					return call, "", "", fmt.Errorf("此 Hailuo 模型仅支持 6 秒或 10 秒")
				}
				if resolution == "1080p" && duration != 6 {
					return call, "", "", fmt.Errorf("此 Hailuo 模型 1080p 仅支持 6 秒")
				}
				task["width"], task["height"] = 1366, 768
				if resolution == "1080p" {
					task["width"], task["height"] = 1920, 1080
				}
			}
			if strings.HasPrefix(model, "alibaba:") && len(images) > 0 {
				delete(task, "width")
				delete(task, "height")
				task["resolution"] = resolution
			}
		}
		call.Body = []Row{task}
	case "bytedance", "infotoken", "uniart":
		if kind == "video" {
			parts := []Row{{"type": "text", "text": prompt}}
			for i, img := range images {
				role := "first_frame"
				if i > 0 {
					role = "last_frame"
				}
				parts = append(parts, Row{"type": "image_url", "image_url": Row{"url": img}, "role": role})
			}
			call.Body = Row{"model": model, "content": parts, "ratio": ratio, "resolution": resolution, "duration": duration, "generate_audio": false}
		} else {
			if vendor == "infotoken" {
				body["resolution"], body["quality"], body["aspect_ratio"] = strings.ToLower(resolution), quality, ratio
				if len(images) > 0 {
					call.Path = "/v1/images/edits"
					body["image_urls"] = images
				}
				break
			}
			body["response_format"] = "url"
			w, h := inventoryDimensions(resolution, ratio)
			body["size"] = fmt.Sprintf("%dx%d", w, h)
			putImages("image")
		}
	case "copp":
		body["mode"] = "text_to_video"
		if len(images) == 1 {
			body["mode"] = "image_to_video"
			body["image_url"] = images[0]
		} else if len(images) == 2 {
			body["mode"] = "first_last_frame"
			body["image_urls"] = images
		}
		body["ratio"] = ratio
		body["resolution"] = resolution
		body["duration"] = duration
		body["auto_review"] = true
	case "afrifex":
		if strings.HasPrefix(source, "video-fast") {
			body["model"] = strings.ReplaceAll(model, "720p", resolution)
		}
		body["duration"] = duration
		body["aspect_ratio"] = ratio
		if len(images) > 0 {
			body["image_url"] = images[0]
		}
		if len(images) > 1 {
			body["extra_images"] = images[1:]
		}
	case "dimensio":
		body["ratio"] = ratio
		if kind == "image" {
			body["quality"] = map[string]string{"1k": "1080p", "2k": "1440p", "4k": "2160p"}[strings.ToLower(resolution)]
			body["detail_level"] = quality
			putImages("images")
			if len(images) > 0 {
				call.Path = "/v1/images/compositions"
			}
		} else {
			body["duration"] = duration
			body["resolution"] = strings.ToLower(resolution)
			body["functionMode"] = "first_last_frames"
			for i, img := range images {
				body[fmt.Sprintf("image_file_%d", i+1)] = img
			}
		}
	case "dsas":
		payload := Row{"aspectRatio": ratio}
		if len(images) > 0 {
			payload["mode"] = "references"
			payload["imageUrls"] = images
		}
		body["duration"] = duration
		body["metadata"] = Row{"payload": jsonText(payload)}
	case "secure-skill-out", "secure-skill-sepcial":
		call.Form = Row{"model": model, "prompt": prompt, "duration": duration, "ratio": ratio, "resolution": resolution}
		call.Body = nil
		if vendor == "secure-skill-out" {
			call.Form["functionMode"] = "first_last_frames"
			for i, img := range images {
				call.Form[fmt.Sprintf("image_file_%d", i+1)] = img
			}
		}
		if vendor == "secure-skill-sepcial" {
			call.Repeated = map[string][]string{"files": images}
		}
	case "secure-skill":
		if kind == "image" {
			if strings.HasPrefix(model, "gpt-image") {
				call.Path = "/v1/images/async/generations"
				poll = "/v1/images/async/{id}"
				method = "GET"
			}
			body["aspect_ratio"] = ratio
			body["resolution"] = resolution
			body["quality"] = quality
			body["n"] = 1
			putImages("image_urls")
		} else if strings.HasPrefix(model, "wan3") {
			body["duration"] = duration
			body["ratio"] = ratio
			body["resolution"] = strings.ToUpper(resolution)
			media := []Row{}
			for _, img := range images {
				media = append(media, Row{"type": "reference_image", "url": img})
			}
			if len(media) > 0 {
				body["media"] = media
			}
			body["prompt_extend"] = false
		} else if strings.HasPrefix(source, "grok-") {
			call.Path = "/openai/v1/videos"
			body["seconds"] = duration
			body["aspect_ratio"] = ratio
			body["resolution"] = resolution
			putImages("reference_images")
			if vendor == "mikotopro" {
				delete(body, "reference_images")
				body["resolution"] = "720p"
				if len(images) > 0 {
					body["image_url"] = images[0]
				}
				if len(images) > 1 {
					body["reference_image_urls"] = images[1:]
				}
			}
			if vendor == "rolldek" {
				body["seconds"] = fmt.Sprint(duration)
				body["size"] = resolution
			}
			if vendor == "zhubo" {
				delete(body, "reference_images")
				putImages("image_urls")
			}
		} else {
			call.Path = "/v1/videos"
			poll = "/v1/videos/{id}"
			body["duration"] = duration
			body["ratio"] = ratio
			body["resolution"] = resolution
			putImages("referenceImages")
		}
	case "wxart":
		body["duration"] = duration
		body["ratio"] = ratio
		body["resolution"] = resolution
		putImages("referenceImages")
	case "openrouter":
		body["aspect_ratio"] = ratio
		body["resolution"] = resolution
		if kind == "image" {
			body["quality"] = quality
			body["output_format"] = "png"
		} else {
			body["duration"] = duration
			body["generate_audio"] = false
		}
		if len(images) > 0 {
			refs := []Row{}
			for _, img := range images {
				refs = append(refs, Row{"type": "image_url", "image_url": Row{"url": img}})
			}
			body["input_references"] = refs
		}
	case "mxapi":
		call.Body = Row{"mv": model, "gpt_description_prompt": prompt, "make_instrumental": truth(params["instrumental"])}
		if str(params["title"]) != "" {
			object(call.Body)["title"] = params["title"]
		}
		if str(params["style"]) != "" {
			object(call.Body)["tags"] = params["style"]
		}
		if strings.Contains(source, "sfx") {
			call.Body = Row{"mv": model, "title": prompt, "tags": fallback(params["style"], prompt)}
		}
	case "runninghub":
		call.Body = Row{"prompt": prompt, "aspectRatio": ratio, "resolution": strings.ToLower(resolution), "quality": quality}
		if quality == "auto" {
			object(call.Body)["quality"] = "medium"
		}
		if len(images) > 0 {
			call.Path = "/openapi/v2/rhart-image-g-2-official/image-to-image"
			object(call.Body)["imageUrls"] = images
		}
	case "atlascloud":
		native := "youchuan/v8.2/text-to-image"
		if len(images) > 0 {
			native = "youchuan/v8.2/image-to-image"
			body["image"] = images[0]
		}
		if strings.HasPrefix(model, "youchuan/") {
			native = model
		}
		body["model"] = native
		body["aspect_ratio"] = ratio
		body["hd"] = resolution == "2k"
		body["enable_base64_output"] = false
	case "midjourney-api":
		if strings.Contains(source, "midjourney") {
			if inventoryMJReserved.MatchString(prompt) {
				return call, "", "", fmt.Errorf("请通过模型参数选择版本、比例和分辨率，提示词不能覆盖这些选项")
			}
			version := model
			if version != "8.1" && version != "8.2" {
				return call, "", "", fmt.Errorf("此 Midjourney 路由需要配置 8.1 或 8.2 版本")
			}
			flag := " --sd"
			if resolution == "2k" {
				flag = " --hd"
			}
			call.Body = Row{"prompt": prompt + " --ar " + ratio + " --v " + version + flag, "mode": "fast", "timeout": 300}
			if len(images) > 0 {
				object(call.Body)["prompt"] = strings.Join(images, " ") + " " + str(object(call.Body)["prompt"])
			}
		} else {
			body["size"] = strings.ToUpper(resolution)
			body["aspect_ratio"] = ratio
			putImages("referImages")
		}
	case "huanwangai":
		call.Body = Row{"botType": "MID_JOURNEY", "prompt": prompt + " --ar " + ratio}
		if len(images) > 0 {
			object(call.Body)["base64Array"] = images
		}
	default:
		if kind == "video" {
			body["seconds"] = duration
			body["ratio"] = ratio
			body["aspect_ratio"] = ratio
			body["resolution"] = resolution
			putImages("reference_images")
			if vendor == "mikotopro" {
				delete(body, "reference_images")
				body["resolution"] = "720p"
				if len(images) > 0 {
					body["image_url"] = images[0]
				}
				if len(images) > 1 {
					body["reference_image_urls"] = images[1:]
				}
			}
			if vendor == "rolldek" {
				body["seconds"] = fmt.Sprint(duration)
				body["size"] = resolution
			}
			if vendor == "zhubo" {
				delete(body, "reference_images")
				putImages("image_urls")
			}
		} else {
			w, h := inventoryDimensions(resolution, ratio)
			body["size"] = fmt.Sprintf("%dx%d", w, h)
			body["n"] = 1
			body["quality"] = quality
			if vendor == "apiyi" && model == "gpt-image-2-vip" {
				delete(body, "quality")
			}
			if vendor == "mikotopro" {
				body["aspect_ratio"] = ratio
				body["resolution"] = strings.ToLower(resolution)
			}
			if (vendor == "mynewapi" || vendor == "ccgo") && strings.HasPrefix(source, "nano-banana") {
				parts := []Row{{"text": prompt}}
				for _, image := range images {
					if strings.HasPrefix(image, "data:") {
						split := strings.SplitN(image, ",", 2)
						if len(split) != 2 {
							return call, "", "", fmt.Errorf("参考图片数据不正确")
						}
						mt := strings.TrimSuffix(strings.TrimPrefix(split[0], "data:"), ";base64")
						parts = append(parts, Row{"inlineData": Row{"mimeType": mt, "data": split[1]}})
					} else {
						parts = append(parts, Row{"fileData": Row{"mimeType": "image/png", "fileUri": image}})
					}
				}
				call.Path = "/v1beta/models/" + model + ":generateContent"
				if vendor == "mynewapi" {
					call.Header = "x-goog-api-key"
				}
				call.Body = Row{"contents": []Row{{"role": "user", "parts": parts}}, "generationConfig": Row{"responseModalities": []string{"IMAGE", "TEXT"}, "imageConfig": Row{"aspectRatio": ratio, "imageSize": strings.ToUpper(resolution)}}}
			} else if len(files) > 0 {
				call.Path = strings.TrimSuffix(call.Path, "generations") + "edits"
				call.Form = body
				call.Body = nil
				for _, asset := range files {
					field := "image[]"
					if len(files) == 1 || (vendor == "apiyi" && model == "gpt-image-2-vip") {
						field = "image"
					}
					call.Files = append(call.Files, vendorFile{field, asset})
				}
			}
		}
	}
	if override := str(cfg["submit_path_override"]); override != "" {
		call.Path = override
	}
	if override := str(cfg["poll_path_override"]); override != "" {
		poll = override
	}
	return call, poll, method, nil
}

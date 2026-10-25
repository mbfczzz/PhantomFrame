package api

import "strings"

func choice(name, label string, values []any, preferred any) Row {
	options := []any{}
	for _, value := range values {
		if isScalar(value) {
			options = append(options, Row{"l": str(value), "v": value})
		}
	}
	if len(options) == 0 {
		return nil
	}
	def := object(options[0])["v"]
	for _, raw := range options {
		if str(object(raw)["v"]) == str(preferred) {
			def = object(raw)["v"]
		}
	}
	return Row{"name": name, "label": label, "type": "select", "options": options, "default": def}
}
func referenceParameter(kind string, max any) Row {
	p := Row{"name": kind + "Urls", "label": map[string]string{"image": "参考图片", "video": "参考视频", "audio": "参考音频"}[kind], "type": "assets", "itemType": kind}
	if max != nil {
		p["max"] = max
	}
	return p
}
func vendorCatalogBase(vendor string, item, root Row) (string, []any, bool) {
	id := str(item["id"])
	lower := strings.ToLower(id)
	if strings.Contains(lower, "watermark-removal") {
		return "", nil, false
	}
	kind := str(first(item["media_type"], item["kind"], item["type"]))
	if kind != "image" && kind != "video" {
		kind = "video"
		for _, tag := range []string{"image", "banana", "seedream", "flux", "qwen-image"} {
			if strings.Contains(lower, tag) {
				kind = "image"
			}
		}
	}
	caps := object(first(item["capabilities"], item["caps"]))
	if caps["available"] == false || item["enabled"] == false || item["pending"] == true {
		return "", nil, false
	}
	params := []any{}
	add := func(p Row) {
		if p != nil {
			params = append(params, p)
		}
	}
	if vendor == "modelhub" && len(array(item["parameters"])) > 0 {
		for _, raw := range array(item["parameters"]) {
			p := object(raw)
			name := str(p["name"])
			if name == "model" || name == "prompt" {
				continue
			}
			if strings.Contains(name, "file_") || name == "images" {
				continue
			}
			label := map[string]string{"ratio": "比例", "resolution": "分辨率", "duration": "时长", "functionMode": "生成模式", "quality": "质量", "detail_level": "细节", "image_count": "生成数量", "generate_audio": "生成音频", "seed": "随机种子", "negative_prompt": "负面提示词"}[name]
			if label == "" {
				continue
			}
			if name == "image_count" {
				name = "n"
			}
			values := array(p["accepted_values"])
			def := p["default"]
			if p["type"] == "boolean" {
				add(Row{"name": name, "label": label, "type": "boolean", "default": str(def) != "false"})
				continue
			}
			if p["type"] == "number" {
				if def != nil {
					def = num(def)
				}
				for i, value := range values {
					values[i] = num(value)
				}
			}
			if len(values) > 0 {
				add(choice(name, label, values, def))
			} else if p["type"] == "number" {
				r := Row{"name": name, "label": label, "type": "number"}
				if def != nil {
					r["default"] = def
				}
				add(r)
			}
		}
	} else {
		ratios := array(first(caps["ratios"], item["ratios"]))
		if len(ratios) == 0 {
			ratios = []any{"16:9", "9:16", "1:1", "4:3", "3:4"}
		}
		add(choice("aspectRatio", "比例", ratios, "16:9"))
		resolutions := array(first(caps["resolutions"], caps["res"]))
		if len(resolutions) == 0 && vendor == "catcoder" {
			for key := range object(item["image_prices"]) {
				resolution := strings.Split(key, "/")[0]
				exists := false
				for _, r := range resolutions {
					if r == resolution {
						exists = true
					}
				}
				if !exists {
					resolutions = append(resolutions, resolution)
				}
			}
		}
		if len(resolutions) == 0 {
			if kind == "image" {
				resolutions = []any{"1k", "2k", "4k"}
			} else {
				resolutions = []any{"720p", "1080p"}
			}
		}
		if !(vendor == "suda" && kind == "video") && !(vendor == "wxart" && kind == "image") {
			add(choice("resolution", "分辨率", resolutions, first(item["resolution"], "720p")))
		}
		if kind == "video" {
			durations := array(caps["durations"])
			if len(durations) > 0 {
				add(choice("duration", "时长", durations, 5))
			} else {
				min, max := first(vendorValue(caps, "dur.min"), item["min_duration"], 4), first(vendorValue(caps, "dur.max"), item["max_duration"], 15)
				if num(min) <= 0 {
					min = 4
				}
				if num(max) < num(min) {
					max = 15
				}
				def := float64(5)
				if def < num(min) {
					def = num(min)
				}
				add(Row{"name": "duration", "label": "时长", "type": "number", "min": min, "max": max, "default": def})
			}
		}
		if vendor == "catcoder" && kind == "image" {
			qualities := []any{}
			for key := range object(item["image_prices"]) {
				parts := strings.Split(key, "/")
				if len(parts) == 2 {
					exists := false
					for _, q := range qualities {
						if q == parts[1] {
							exists = true
						}
					}
					if !exists {
						qualities = append(qualities, parts[1])
					}
				}
			}
			add(choice("quality", "质量", qualities, "medium"))
		}
	}
	if vendor == "openvideo" {
		add(Row{"name": "faceEnabled", "label": "启用人脸处理", "type": "boolean", "default": false})
		modes := array(caps["modes"])
		filtered := []any{}
		for _, mode := range modes {
			if mode == "omni_reference" || mode == "first_last_frames" || mode == "multi_frame" {
				filtered = append(filtered, mode)
			}
		}
		add(choice("function_mode", "生成模式", filtered, "omni_reference"))
		channels := []any{}
		for _, raw := range array(root["channels"]) {
			channel := object(raw)
			name := str(first(channel["name"], channel["id"]))
			if name == "" {
				continue
			}
			models := object(channel["models"])
			found := false
			for _, v := range array(models[kind]) {
				if str(first(object(v)["id"], v)) == id {
					found = true
				}
			}
			for _, v := range array(channel["models"]) {
				if str(first(object(v)["id"], v)) == id {
					found = true
				}
			}
			if found {
				channels = append(channels, name)
			}
		}
		add(choice("channel", "上游线路", channels, "default"))
	}
	if vendor == "suda" && kind == "video" {
		add(choice("mode", "生成模式", []any{"references", "frames"}, "references"))
	}
	for _, media := range []string{"image", "video", "audio"} {
		if kind == "image" && media != "image" {
			continue
		}
		max := first(caps["max_"+map[string]string{"image": "images", "video": "videos", "audio": "audio"}[media]], item["max_"+map[string]string{"image": "images", "video": "videos", "audio": "audio"}[media]])
		if vendor == "modelhub" && kind == "video" {
			max = map[string]int{"image": 9, "video": 3, "audio": 3}[media]
			if strings.Contains(lower, "2.5") {
				max = map[string]int{"image": 30, "video": 10, "audio": 10}[media]
			}
		}
		add(referenceParameter(media, max))
	}
	return kind, params, true
}

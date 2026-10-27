package api

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Model contracts copied from the user's apiRouter catalog, without credentials.
//
//go:embed inventory_kinovi.json
var inventoryKinoviData []byte
var inventoryKinoviCatalog = func() []Row {
	var rows []Row
	if e := json.Unmarshal(inventoryKinoviData, &rows); e != nil {
		panic(e)
	}
	return rows
}()

func inventoryKinoviSpec(model, upstream string) Row {
	for _, spec := range inventoryKinoviCatalog {
		for _, key := range []string{model, upstream} {
			if key == str(spec["id"]) {
				return spec
			}
			for _, field := range []string{"aliases", "upstreams"} {
				for _, value := range array(spec[field]) {
					if str(value) == key {
						return spec
					}
				}
			}
		}
	}
	return nil
}

func inventoryKinoviParams(r InventoryRoute, params []any) []any {
	spec := inventoryKinoviSpec(r.Model, r.Upstream)
	if spec == nil {
		return params
	}
	result := []any{}
	defaults := object(spec["defaults"])
	for _, raw := range params {
		p := clone(object(raw))
		name := str(p["name"])
		switch name {
		case "aspectRatio", "resolution":
			field := "aspects"
			preferred := p["default"]
			if name == "resolution" {
				field = "resolutions"
				preferred = first(defaults["resolution"], defaults["outputResolution"], preferred)
			}
			values := array(spec[field])
			if len(values) == 0 {
				continue
			}
			p = choice(name, str(p["label"]), values, preferred)
		case "duration":
			p["min"] = spec["minDuration"]
			p["max"] = spec["maxDuration"]
			p["default"] = max(int64(5), integer(spec["minDuration"]))
		case "imageUrls":
			maxImages := integer(spec["maxImages"])
			if spec["profile"] == "kling" {
				maxImages = 3
			}
			if maxImages == 0 {
				continue
			}
			if maxImages < 0 {
				delete(p, "max")
			} else {
				p["max"] = maxImages
			}
		}
		result = append(result, p)
	}
	if r.Modality == "video" {
		if n := integer(spec["maxVideos"]); n > 0 {
			result = append(result, referenceParameter("video", n))
		}
		if n := integer(spec["maxAudios"]); n > 0 {
			result = append(result, referenceParameter("audio", n))
		}
	}
	return result
}

func inventoryKinoviBody(source, model, prompt string, params Row, refs map[string][]string) (Row, error) {
	spec := inventoryKinoviSpec(source, model)
	if spec == nil {
		return nil, fmt.Errorf("供应商 Kinovi 模型未找到对应协议")
	}
	if maximum := integer(spec["maxPrompt"]); maximum > 0 && int64(utf8.RuneCountInString(prompt)) > maximum {
		return nil, fmt.Errorf("供应商 Kinovi 此模型提示词最多 %d 个字符", maximum)
	}
	input := Row{"prompt": prompt}
	profile := str(spec["profile"])
	kind := str(spec["kind"])
	for _, pair := range []struct{ kind, key string }{{"image", "maxImages"}, {"video", "maxVideos"}, {"audio", "maxAudios"}} {
		maximum := integer(spec[pair.key])
		if maximum >= 0 && int64(len(refs[pair.kind])) > maximum {
			return nil, fmt.Errorf("供应商 Kinovi 参考素材数量超过限制")
		}
	}
	if ratio := str(params["aspectRatio"]); ratio != "" {
		input["aspectRatio"] = ratio
	}
	if kind == "image" {
		if resolution := str(params["resolution"]); resolution != "" {
			input["resolution"] = resolution
		}
		if profile == "gpt" {
			input["quality"] = first(params["quality"], "auto")
		}
		if len(refs["image"]) > 0 {
			input["uploadedUrls"] = refs["image"]
		}
	} else if kind == "audio" {
		input["mv"] = "chirp-v5-5"
		input["isStorage"] = true
		input["instrumental"] = truth(params["instrumental"])
		if str(params["title"]) != "" {
			input["title"] = params["title"]
		}
		if str(params["style"]) != "" {
			input["style"] = params["style"]
		}
	} else {
		duration := integer(params["duration"])
		if duration < integer(spec["minDuration"]) || duration > integer(spec["maxDuration"]) {
			return nil, fmt.Errorf("供应商 Kinovi 时长超出模型范围")
		}
		input["duration"] = duration
		if profile == "seedance20" {
			input["duration"] = fmt.Sprint(duration)
		}
		input["outputResolution"] = params["resolution"]
		imageKey := "imageUrls"
		switch profile {
		case "wan":
			if !strings.Contains(model, "-to-video") {
				mode := "text"
				if len(refs["image"])+len(refs["video"])+len(refs["audio"]) > 0 {
					mode = "ref"
				}
				for _, native := range array(spec["upstreams"]) {
					if strings.HasSuffix(str(native), mode+"-to-video") {
						model = str(native)
						break
					}
				}
			}
			if strings.HasSuffix(model, "text-to-video") && len(refs["image"])+len(refs["video"])+len(refs["audio"]) > 0 {
				return nil, fmt.Errorf("此 Kinovi 上游模型仅支持文生视频")
			}
			input["audio"] = true
		case "kling":
			imageKey = "elementUrls"
			if len(refs["image"]) > 3 {
				return nil, fmt.Errorf("Kling 参考元素最多 3 个")
			}
			input["klingMode"] = "std"
			if strings.Contains(source, "pro") || strings.Contains(source, "4k") {
				input["klingMode"] = "pro"
			}
			if strings.Contains(source, "4k") {
				input["upscaleResolution"] = "4k"
				input["outputResolution"] = "1080p"
			}
		case "happyhorse":
			imageKey = "uploadedUrls"
			input["happyhorseMode"] = "t2v"
			if len(refs["image"]) > 0 {
				input["happyhorseMode"] = "r2v"
			}
			if len(refs["video"]) > 0 {
				input["happyhorseMode"] = "video-edit"
				input["videoUrl"] = refs["video"][0]
			}
		default:
			input["mode"] = "reference"
		}
		if len(refs["image"]) > 0 {
			input[imageKey] = refs["image"]
		}
		if len(refs["video"]) > 0 && profile != "happyhorse" {
			input["videoUrls"] = refs["video"]
		}
		if len(refs["audio"]) > 0 {
			input["audioUrls"] = refs["audio"]
		}
	}
	return Row{"model": model, "inputs": input}, nil
}

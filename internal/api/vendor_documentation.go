package api

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
)

//go:embed vendor_documentation.json
var documentationJSON []byte

var vendorDocumentation = func() Row { var data Row; must(json.Unmarshal(documentationJSON, &data)); return data }()

func documentationFor(vendor, model string) Row {
	return object(object(object(vendorDocumentation["models"])[vendor])[model])
}

func vendorCatalogEntry(vendor string, item, root Row) (string, []any, bool) {
	kind, parameters, ok := vendorCatalogDefinition(vendor, item, root)
	return kind, imageRequestParameters(kind, parameters), ok
}
func vendorCatalogDefinition(vendor string, item, root Row) (string, []any, bool) {
	if vendor == "opencrow" {
		model := str(item["id"])
		spec := openCrowDefinition(model)
		if spec == nil || item["enabled"] == false {
			return "", nil, false
		}
		return str(spec["kind"]), array(jsonText(openCrowParameters(model))), true
	}
	if vendor == "maitoken" {
		definition := maiTokenDefinition(str(item["id"]))
		if definition == nil || item["enabled"] == false {
			return "", nil, false
		}
		return str(definition["kind"]), array(jsonText(definition["parameters"])), true
	}
	if vendor == "rolldek" {
		definition := rolldekDefinition(str(item["id"]))
		if definition == nil || item["enabled"] == false || item["pending"] == true {
			return "", nil, false
		}
		return str(definition["kind"]), array(jsonText(definition["parameters"])), true
	}
	facts := documentationFor(vendor, str(item["id"]))
	if facts["kind"] != nil {
		item = clone(item)
		item["type"], item["kind"], item["media_type"] = facts["kind"], facts["kind"], facts["kind"]
	}
	kind, parameters, ok := vendorCatalogBase(vendor, item, root)
	if !ok {
		return kind, parameters, ok
	}
	parameters = array(jsonText(parameters))
	// Structured live capabilities take precedence over the reviewed documentation snapshot.
	live := vendor == "catcoder" || vendor == "modelhub"
	replace := func(name string, definition Row) {
		for i, raw := range parameters {
			if object(raw)["name"] == name {
				parameters[i] = definition
				return
			}
		}
		parameters = append(parameters, definition)
	}
	has := func(name string) bool {
		for _, raw := range parameters {
			if object(raw)["name"] == name {
				return true
			}
		}
		return false
	}
	for _, spec := range []struct{ key, name, label string }{{"ratios", "aspectRatio", "比例"}, {"resolutions", "resolution", "分辨率"}, {"quality", "quality", "质量"}, {"detailLevel", "detail_level", "细节"}} {
		if spec.key == "ratios" && has("ratio") {
			spec.name = "ratio"
		}
		if vendor == "suda" && kind == "video" && spec.key == "resolutions" {
			continue
		}
		values := object(facts[spec.key])
		if len(array(values["values"])) > 0 && (!live || !has(spec.name)) {
			replace(spec.name, choice(spec.name, spec.label, array(values["values"]), values["default"]))
		}
	}
	if duration := object(facts["duration"]); kind == "video" && len(duration) > 0 && (!live || !has("duration")) {
		if values := array(duration["values"]); len(values) > 0 {
			replace("duration", choice("duration", "时长", values, float64(5)))
		} else if duration["min"] != nil && duration["max"] != nil {
			def := float64(5)
			if num(duration["min"]) > def {
				def = num(duration["min"])
			}
			replace("duration", Row{"name": "duration", "label": "时长", "type": "number", "min": duration["min"], "max": duration["max"], "default": def})
		}
	}
	for _, raw := range parameters {
		p := object(raw)
		if p["type"] != "assets" {
			continue
		}
		media := str(p["itemType"])
		if limit := object(facts["references"])[media]; limit != nil && (!live || p["max"] == nil) {
			p["max"] = limit
		}
		if p["max"] == nil {
			p["limitSource"] = "undocumented"
		} else {
			p["limitSource"] = "documentation"
		}
		total := facts["maxTotal"]
		if vendor == "catcoder" && item["max_total_media"] != nil {
			total = item["max_total_media"]
		}
		if num(total) > 0 {
			p["maxTotal"] = total
		}
		if media == "video" && facts["videoMaxByResolution"] != nil {
			p["maxByResolution"] = facts["videoMaxByResolution"]
		}
	}
	return kind, parameters, true
}

func documentedModelDescription(vendor, upstream, kind string, parameters []any) string {
	return imageRequestDescription(kind, upstream, vendorModelDescription(vendor, upstream, kind, parameters))
}
func vendorModelDescription(vendor, upstream, kind string, parameters []any) string {
	if vendor == "opencrow" {
		description := "OpenCrow · " + str(openCrowDefinition(upstream)["name"]) + "；同步后需确认本站售价再启用"
		if openCrowDefinition(upstream)["profile"] == "motion" {
			description += "。形象图须为 PNG/JPEG，宽高至少 300px；动作视频须为 MP4，宽高 340–3850px，人物朝向 image 时 3–10 秒、video 时 3–30 秒。"
		}
		return description
	}
	if vendor == "maitoken" {
		return str(maiTokenDefinition(upstream)["description"])
	}
	if vendor == "rolldek" {
		return str(rolldekDefinition(upstream)["description"])
	}
	facts := documentationFor(vendor, upstream)
	parts := []string{}
	if kind == "image" {
		parts = append(parts, "图片生成")
	} else {
		parts = append(parts, "视频生成")
	}
	unknown := []string{}
	total := float64(0)
	for _, raw := range parameters {
		p := object(raw)
		if p["type"] != "assets" {
			continue
		}
		media := str(p["itemType"])
		label := map[string]string{"image": "参考图片", "video": "参考视频", "audio": "参考音频"}[media]
		unit := "段"
		if media == "image" {
			unit = "张"
		}
		if p["max"] == nil {
			unknown = append(unknown, label)
			continue
		}
		if num(p["max"]) == 0 {
			parts = append(parts, "不支持"+label)
		} else {
			parts = append(parts, label+"最多 "+str(p["max"])+" "+unit)
		}
		if n := num(p["maxTotal"]); n > 0 && (total == 0 || n < total) {
			total = n
		}
	}
	if total > 0 {
		parts = append(parts, "参考素材合计最多 "+str(total)+" 个")
	}
	for _, raw := range parameters {
		p := object(raw)
		name := str(p["name"])
		factKey := map[string]string{"resolution": "resolutions", "duration": "duration", "quality": "quality", "detail_level": "detailLevel"}[name]
		if facts[factKey] == nil {
			continue
		}
		if name != "resolution" && name != "duration" && name != "quality" && name != "detail_level" {
			continue
		}
		values := []string{}
		for _, raw := range array(p["options"]) {
			value := raw
			if o, ok := raw.(map[string]any); ok {
				value = o["v"]
			}
			values = append(values, str(value))
		}
		if name == "duration" {
			if len(values) > 3 {
				numbers := []float64{}
				for _, v := range values {
					numbers = append(numbers, num(v))
				}
				sort.Float64s(numbers)
				contiguous := true
				for i := 1; i < len(numbers); i++ {
					if numbers[i] != numbers[i-1]+1 {
						contiguous = false
					}
				}
				if contiguous {
					values = []string{str(numbers[0]) + "–" + str(numbers[len(numbers)-1])}
				}
			}
			if len(values) == 0 && p["min"] != nil && p["max"] != nil {
				values = []string{str(p["min"]) + "–" + str(p["max"])}
			}
			if len(values) > 0 {
				parts = append(parts, "时长 "+strings.Join(values, " / ")+" 秒")
			}
		} else if len(values) > 0 {
			for i, v := range values {
				if strings.HasSuffix(strings.ToLower(v), "k") {
					values[i] = strings.ToUpper(v)
				}
			}
			parts = append(parts, str(p["label"])+" "+strings.Join(values, " / "))
		}
	}
	if facts["fixedResolution"] != nil {
		parts = append(parts, "固定输出 "+str(facts["fixedResolution"]))
	}
	if len(unknown) > 0 {
		parts = append(parts, strings.Join(unknown, "、")+"数量上限未公开")
	}
	text := strings.Join(parts, "；") + "。"
	for _, note := range array(facts["notes"]) {
		if !strings.Contains(str(note), "未公开") {
			text += " " + str(note)
		}
	}
	return text
}

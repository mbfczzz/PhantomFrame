package api

import (
	"math"
	"sort"
)

func pricingVersion(rows []Row) string {
	ordered := append([]Row{}, rows...)
	sort.Slice(ordered, func(i, j int) bool { return str(ordered[i]["id"]) < str(ordered[j]["id"]) })
	return hash(jsonText(ordered))
}

func validatePriceRule(m Row, p Row) Row {
	label := textField(p, "label", 1, 80)
	unit := enumField(p, "unit", "request", "second", "image")
	if m["type"] == "image" && unit != "request" {
		fail(400, "图片模型只支持按次计费")
	}
	value := priceField(p, "credits")
	enabled := boolField(p, "enabled", true)
	definitions := map[string]Row{}
	for _, raw := range array(m["parameters"]) {
		parameter := object(raw)
		definitions[str(parameter["name"])] = parameter
	}
	if unit == "second" {
		if m["type"] != "video" {
			fail(400, "只有视频模型支持按秒计费")
		}
		duration := definitions["duration"]
		if duration == nil || (duration["type"] != "number" && duration["type"] != "select") {
			fail(400, "按秒计费需要配置时长参数 duration")
		}
	}
	if unit == "image" && m["type"] != "image" {
		fail(400, "视频模型支持按次或按秒计费，不能按张计费")
	}
	conditions := mapField(p, "conditions")
	for key, value := range conditions {
		if !isScalar(value) {
			fail(400, "计费条件必须是字符串、数字或布尔值")
		}
		parameter := definitions[key]
		if parameter == nil {
			fail(400, "模型没有 "+key+" 参数")
		}
		switch parameter["type"] {
		case "assets":
			fail(400, "参考素材不能作为单价匹配条件")
		case "select":
			found := false
			for _, option := range array(parameter["options"]) {
				actual := option
				if record, ok := option.(map[string]any); ok {
					actual = record["v"]
				}
				if str(actual) == str(value) {
					conditions[key] = actual
					found = true
					break
				}
			}
			if !found {
				fail(400, str(parameter["label"])+"价格条件不在可选规格内")
			}
		case "boolean":
			if _, ok := value.(bool); !ok {
				fail(400, str(parameter["label"])+"价格条件必须为布尔值")
			}
		case "number":
			number, ok := value.(float64)
			if !ok || math.IsNaN(number) || math.IsInf(number, 0) || (parameter["min"] != nil && number < num(parameter["min"])) || (parameter["max"] != nil && number > num(parameter["max"])) {
				fail(400, str(parameter["label"])+"价格条件超出范围")
			}
		case "text":
			textField(conditions, key, 0, 4000)
		}
	}
	extras := billingExtras(p)
	if num(extras["videoOutputSecondPrice"]) > 0 && (m["type"] != "video" || definitions["duration"] == nil) {
		fail(400, "按生成时长收取参考视频附加费，需要视频模型及 duration 参数")
	}
	return Row{"label": label, "unit": unit, "credits": units(value), "unit_price_micros": int64(math.Round(value * 1000000)), "billing_extras": extras, "conditions": conditions, "enabled": enabled}
}

func writePriceRule(s Store, model Row, pid string, body Row, update bool) {
	mid := model["id"]
	if update {
		old := s.One("SELECT model_id,billing_extras FROM prices WHERE id=?", pid)
		if old == nil {
			fail(404, "价格规则不存在")
		}
		if old["model_id"] != mid {
			fail(400, "价格规则不能移动到其他模型")
		}
		if _, present := body["extras"]; !present {
			body = clone(body)
			body["extras"] = object(old["billing_extras"])
		}
	}
	p := validatePriceRule(model, body)
	for _, old := range s.All("SELECT * FROM prices WHERE model_id=? AND id!=?", mid, pid) {
		if truth(p["enabled"]) && truth(old["enabled"]) && old["unit"] != p["unit"] {
			fail(400, "同一模型的启用价格规则需使用同一种计费方式，请先停用其他计费方式的规则")
		}
		if old["unit"] == p["unit"] && jsonText(object(old["conditions"])) == jsonText(p["conditions"]) {
			fail(400, "相同条件的价格规则已存在")
		}
	}
	if update {
		s.Exec("UPDATE prices SET label=?,unit=?,credits=?,unit_price_micros=?,billing_extras=?,conditions=?,enabled=? WHERE id=?", p["label"], p["unit"], p["credits"], p["unit_price_micros"], jsonText(p["billing_extras"]), jsonText(p["conditions"]), p["enabled"], pid)
	} else {
		s.Exec("INSERT INTO prices(id,model_id,label,unit,credits,unit_price_micros,billing_extras,conditions,enabled) VALUES(?,?,?,?,?,?,?,?,?)", pid, mid, p["label"], p["unit"], p["credits"], p["unit_price_micros"], jsonText(p["billing_extras"]), jsonText(p["conditions"]), p["enabled"])
	}
}

func (a *App) saveModelPrices(r *Request) any {
	body := r.Body()
	version := textField(body, "expectedVersion", 64, 64)
	rules := listField(body, "prices", 200)
	if len(rules) == 0 {
		fail(400, "请至少填写一个规格的单价")
	}
	var result Row
	a.Tx(func(s Store) {
		model := s.One("SELECT * FROM models WHERE id=? FOR UPDATE", r.ID())
		if model == nil {
			fail(404, "模型不存在")
		}
		if version != pricingVersion(s.All("SELECT * FROM prices WHERE model_id=?", r.ID())) {
			fail(409, "模型价格已被修改，请重新打开定价窗口后保存")
		}
		seen := map[string]bool{}
		for _, raw := range rules {
			p, ok := raw.(map[string]any)
			if !ok {
				fail(400, "价格规则格式不正确")
			}
			pid := textField(p, "id", 0, 100, "")
			update := pid != ""
			if !update {
				pid = id()
			}
			if seen[pid] {
				fail(400, "同一价格规则不能重复提交")
			}
			seen[pid] = true
			writePriceRule(s, model, pid, p, update)
		}
		s.Audit(r.UID(), "model.prices.save", Row{"modelId": r.ID(), "rules": len(rules)})
		result = s.ModelView(model)
	})
	return result
}

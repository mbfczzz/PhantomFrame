package api

import (
	"fmt"
	"math"
)

func inventoryRouteFromRow(row Row) InventoryRoute {
	return InventoryRoute{ID: str(row["id"]), Vendor: str(row["vendor"]), Model: str(row["model"]), Upstream: str(row["upstream_model"]), Modality: str(row["modality"]), Config: object(row["config"])}
}

// Persisted/admin-edited schemas may lag behind adapter capabilities. Validate
// against the adapter as well as the UI schema, before charging or uploading.
func inventoryValidateNativeParameters(provider, params Row) error {
	cfg := object(provider["config"])
	r := InventoryRoute{Vendor: str(cfg["vendor"]), Model: str(cfg["sourceModel"]), Upstream: str(first(cfg["upstreamModel"], provider["upstreamModel"])), Modality: str(provider["type"])}
	definitions := map[string]Row{}
	for _, raw := range inventoryParameters(r) {
		p := object(raw)
		definitions[str(p["name"])] = p
		value := params[str(p["name"])]
		if truth(p["required"]) && p["type"] != "assets" && (value == nil || value == "") {
			return fmt.Errorf("供应商需要填写 %s", str(p["label"]))
		}
	}
	for name, value := range params {
		if value == nil {
			continue
		}
		if r.Modality == "image" && imageCountParameter(name) && num(value) == 1 {
			continue
		}
		p := definitions[name]
		valid := p != nil
		if valid {
			switch p["type"] {
			case "select":
				valid = false
				for _, option := range array(p["options"]) {
					if item, ok := option.(map[string]any); ok {
						option = item["v"]
					}
					if str(option) == str(value) {
						valid = true
						break
					}
				}
			case "number":
				n := num(value)
				valid = !math.IsNaN(n) && !math.IsInf(n, 0) && (p["min"] == nil || n >= num(p["min"])) && (p["max"] == nil || n <= num(p["max"])) && (!truth(p["integer"]) || n == math.Trunc(n))
			case "assets":
				valid = p["max"] == nil || float64(len(array(value))) <= num(p["max"])
			}
		}
		if !valid {
			return fmt.Errorf("供应商不支持当前 %s 参数，请在供应商路由中更新参数规范并重新确认价格", name)
		}
	}
	return nil
}

func inventoryImageOutputLimit(provider Row) int {
	return max(imageOutputLimit(str(provider["upstreamModel"])), imageOutputLimit(str(object(provider["config"])["sourceModel"])))
}

func inventoryRejectLegacyCatalog(provider Row) {
	if provider == nil {
		fail(404, "渠道不存在")
	}
	if provider["protocol"] == "inventory" {
		fail(400, "清单渠道请在供应商路由中管理，不能通过旧模型目录同步、发布或设置密钥")
	}
}

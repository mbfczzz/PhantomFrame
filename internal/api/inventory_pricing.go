package api

import "sort"

type inventoryPriceSource struct {
	model  Row
	prices []Row
}

// Take the snapshot before importing: prices inherited in this batch cannot
// become sources for another route. Demo prices never establish retail prices.
func inventoryPriceSources(s Store) []inventoryPriceSource {
	result := []inventoryPriceSource{}
	prices := map[string][]Row{}
	for _, p := range s.All("SELECT * FROM prices ORDER BY id") {
		prices[str(p["model_id"])] = append(prices[str(p["model_id"])], p)
	}
	for _, m := range s.All("SELECT m.*,r.model source_model FROM models m JOIN providers p ON p.id=m.provider_id LEFT JOIN inventory_routes r ON r.id=m.inventory_route_id WHERE p.protocol<>'demo' ORDER BY m.id") {
		if rows := prices[str(m["id"])]; len(rows) > 0 {
			result = append(result, inventoryPriceSource{m, rows})
		}
	}
	return result
}

func inheritInventoryPrices(s Store, r InventoryRoute, sources []inventoryPriceSource) string {
	mid := inventoryModelID(r.ID)
	m := s.One("SELECT * FROM models WHERE id=? FOR UPDATE", mid)
	if s.One("SELECT id FROM prices WHERE model_id=? LIMIT 1", mid) != nil {
		return "preserved"
	}
	var selected []Row
	fingerprint := ""
	for _, source := range sources {
		old := source.model
		if old["type"] != r.Modality || str(old["id"]) == mid {
			continue
		}
		match := false
		for _, key := range []string{"id", "name", "upstream_model", "source_model"} {
			if str(old[key]) == r.Model {
				match = true
			}
		}
		if !match {
			continue
		}
		normalized := []string{}
		active := false
		units := ""
		seen := map[string]bool{}
		err := try(func() {
			for _, raw := range source.prices {
				p := validatePriceRule(m, priceView(clone(raw)))
				condition := str(p["unit"]) + ":" + jsonText(p["conditions"])
				if seen[condition] {
					fail(400, "重复条件")
				}
				seen[condition] = true
				if truth(p["enabled"]) {
					active = true
					if units != "" && units != str(p["unit"]) {
						fail(400, "计费单位冲突")
					}
					units = str(p["unit"])
				}
				// Compare financial meaning, not labels or generated rule IDs.
				delete(p, "label")
				normalized = append(normalized, jsonText(p))
			}
		})
		if err != nil || !active {
			return "同名模型价格与参数不兼容或没有启用价格"
		}
		sort.Strings(normalized)
		next := jsonText(normalized)
		if fingerprint != "" && fingerprint != next {
			return "同名模型存在不同价格，需人工确定"
		}
		fingerprint, selected = next, source.prices
	}
	if selected == nil {
		return "没有可沿用的同名模型价格"
	}
	for _, p := range selected {
		// Copy integer money fields directly; never round a retail price again.
		s.Exec("INSERT INTO prices(id,model_id,label,unit,credits,unit_price_micros,billing_extras,conditions,enabled) VALUES(?,?,?,?,?,?,?,?,?)", id(), mid, p["label"], p["unit"], p["credits"], p["unit_price_micros"], p["billing_extras"], p["conditions"], p["enabled"])
	}
	return "inherited"
}

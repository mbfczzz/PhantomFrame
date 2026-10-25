package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

func comparablePrices(prices []any) []Row {
	result := []Row{}
	for _, raw := range prices {
		p := object(raw)
		result = append(result, Row{"id": p["id"], "label": p["label"], "unit": p["unit"], "credits": p["credits"], "conditions": object(p["conditions"]), "enabled": p["enabled"], "extras": object(p["extras"])})
	}
	sort.Slice(result, func(i, j int) bool { return str(result[i]["id"]) < str(result[j]["id"]) })
	return result
}

func (a *App) ImportSupplierPrices(path string, apply bool) (Row, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var plan Row
	if e = json.Unmarshal(b, &plan); e != nil {
		return nil, e
	}
	if integer(plan["version"]) != 1 || integer(plan["markupPercent"]) != 20 {
		fail(400, "不支持的供应商定价计划")
	}
	count := 0
	byVendor := Row{}
	report := Row{"applied": apply, "pending": array(plan["pending"]), "skippedTiers": array(plan["skippedTiers"]), "conversions": plan["conversions"]}
	e = try(func() {
		a.Tx(func(s Store) {
			if integer(s.One("SELECT count(*) n FROM jobs WHERE status IN ('queued','running')")["n"]) > 0 {
				fail(409, "请等待生成任务结束再更新售价")
			}
			usersBefore := jsonText(s.All("SELECT id,balance FROM users ORDER BY id"))
			ledgerBefore := jsonText(s.All("SELECT * FROM transactions ORDER BY id"))
			prepared := []Row{}
			for _, raw := range array(plan["models"]) {
				item := object(raw)
				before := object(item["before"])
				model := s.One("SELECT * FROM models WHERE id=? FOR UPDATE", item["id"])
				if model == nil {
					fail(409, "模型已被删除")
				}
				for _, key := range []string{"id", "name", "type", "provider_id", "upstream_model", "description"} {
					if str(model[key]) != str(before[key]) {
						fail(409, "模型信息已变更，请重新生成价格计划")
					}
				}
				if truth(model["enabled"]) != truth(before["enabled"]) || jsonText(array(model["parameters"])) != jsonText(array(before["parameters"])) {
					fail(409, "模型状态或参数已变更，请重新生成价格计划")
				}
				view := s.ModelView(model)
				if jsonText(comparablePrices(array(view["prices"]))) != jsonText(comparablePrices(array(before["prices"]))) {
					fail(409, "模型价格已变更，请重新生成价格计划")
				}
				wanted := object(item["model"])
				model["parameters"] = jsonText(imageRequestParameters(str(model["type"]), validateParameters(array(wanted["parameters"]))))
				seen := map[string]bool{}
				for _, price := range array(item["rules"]) {
					p := object(price)
					validated := validatePriceRule(model, p)
					key := str(validated["unit"]) + jsonText(validated["conditions"])
					if seen[key] {
						fail(400, "定价计划含重复规格")
					}
					seen[key] = true
					count++
				}
				byVendor[str(item["vendor"])] = integer(byVendor[str(item["vendor"])]) + 1
				prepared = append(prepared, Row{"model": model, "rules": item["rules"]})
			}
			if apply {
				backup := Row{"models": s.All("SELECT * FROM models ORDER BY id"), "prices": s.All("SELECT * FROM prices ORDER BY id")}
				directory := filepath.Join(a.Config.DataDir, "backups")
				must(os.MkdirAll(directory, 0700))
				backupPath := filepath.Join(directory, "supplier-pricing-"+id()+".encrypted.json")
				must(os.WriteFile(backupPath, []byte(a.encrypt(jsonText(backup))), 0600))
				report["backup"] = backupPath
				for _, item := range prepared {
					m := object(item["model"])
					s.Exec("UPDATE models SET parameters=? WHERE id=?", m["parameters"], m["id"])
					s.Exec("DELETE FROM prices WHERE model_id=?", m["id"])
					for _, raw := range array(item["rules"]) {
						p := object(raw)
						pid := "supplier-" + hash(str(m["id"]) + str(p["unit"]) + jsonText(p["conditions"]))[:32]
						writePriceRule(s, m, pid, p, false)
					}
					s.Exec("UPDATE models SET enabled=1 WHERE id=?", m["id"])
					for _, raw := range array(item["rules"]) {
						p := object(raw)
						q := a.quote(s, str(m["id"]), "", clone(object(p["conditions"])))
						if priceMicros(q.Rate) != priceMicros(validatePriceRule(m, p)) {
							fail(500, "导入报价与计划不一致")
						}
					}
					s.Exec("UPDATE models SET enabled=? WHERE id=?", truth(m["enabled"]), m["id"])
				}
				if usersBefore != jsonText(s.All("SELECT id,balance FROM users ORDER BY id")) || ledgerBefore != jsonText(s.All("SELECT * FROM transactions ORDER BY id")) {
					panic("account data unexpectedly changed")
				}
				admin := s.One("SELECT id FROM users WHERE role='admin' ORDER BY created_at LIMIT 1")
				if admin != nil {
					s.Audit(str(admin["id"]), "maintenance.supplier_pricing", Row{"markupPercent": 20, "models": len(prepared), "rules": count, "pending": plan["pending"], "backup": backupPath})
				}
			}
			report["models"] = len(prepared)
			report["rules"] = count
			report["byVendor"] = byVendor
			report["accountsPreserved"] = true
		})
	})
	return report, e
}

// Validate every configured tier without creating a job or contacting a supplier.
// Temporary model enablement is rolled back and cannot be observed by other connections.
func (a *App) VerifySupplierPrices(path string) (Row, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var plan Row
	if e = json.Unmarshal(b, &plan); e != nil {
		return nil, e
	}
	tx, e := a.DB.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	s := Store{tx}
	checked := 0
	e = try(func() {
		admin := s.One("SELECT id FROM users WHERE role='admin' LIMIT 1")
		for _, raw := range array(plan["models"]) {
			item := object(raw)
			s.Exec("UPDATE models SET enabled=1 WHERE id=?", item["id"])
			for _, rule := range array(item["rules"]) {
				p := object(rule)
				params := clone(object(p["conditions"]))
				q := a.quote(s, str(item["id"]), str(admin["id"]), params)
				if priceMicros(q.Rate) != priceMicros(validatePriceRule(q.Model, p)) {
					fail(500, "报价未匹配导入单价")
				}
				checked++
			}
		}
	})
	return Row{"verifiedRules": checked, "supplierGenerationCalls": 0}, e
}

package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// BackfillVendorDocumentation only replaces fields still equal to their import snapshot.
// Calling this maintenance method never changes IDs, aliases, prices, channels or status.
func (a *App) BackfillVendorDocumentation(snapshotPath, sourceDirectory string, apply bool) (Row, error) {
	var snapshot Row
	b, err := os.ReadFile(snapshotPath)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &snapshot); err != nil {
		return nil, err
	}
	baseline := map[string]Row{}
	for _, raw := range array(snapshot["existingModels"]) {
		m := object(raw)
		baseline[str(m["provider_id"])+"|"+str(m["upstream_model"])] = m
	}
	for _, raw := range array(snapshot["plan"]) {
		p := object(raw)
		e := object(p["entry"])
		baseline[str(p["providerId"])+"|"+str(e["upstreamModel"])] = e
	}
	sources := map[string]Row{}
	for _, p := range a.Store.All("SELECT protocol FROM providers WHERE protocol<>'media-upload'") {
		vendor := str(p["protocol"])
		b, e := os.ReadFile(filepath.Join(sourceDirectory, vendor+"-models.json"))
		if e != nil {
			return nil, e
		}
		var raw Row
		if e = json.Unmarshal(b, &raw); e != nil {
			return nil, e
		}
		sources[vendor] = raw
	}
	changes := []Row{}
	preserved := []Row{}
	counts := Row{}
	err = try(func() {
		a.Tx(func(s Store) {
			if apply && integer(s.One("SELECT count(*) n FROM jobs WHERE status IN ('queued','running')")["n"]) != 0 {
				fail(409, "请等待生成任务结束后再同步模型默认说明")
			}
			for _, table := range []string{"provider_models", "models"} {
				for _, current := range s.All("SELECT * FROM " + table + " ORDER BY id FOR UPDATE") {
					provider := s.One("SELECT protocol FROM providers WHERE id=?", current["provider_id"])
					vendor := str(provider["protocol"])
					root := sources[vendor]
					var item Row
					for _, raw := range array(root["data"]) {
						if object(raw)["id"] == current["upstream_model"] {
							item = object(raw)
							break
						}
					}
					if item == nil {
						continue
					}
					base := baseline[str(current["provider_id"])+"|"+str(current["upstream_model"])]
					if base == nil {
						preserved = append(preserved, Row{"table": table, "id": current["id"], "reason": "no import snapshot"})
						continue
					}
					kind, generated, ok := vendorCatalogEntry(vendor, item, root)
					if !ok {
						continue
					}
					generated = validateParameters(array(jsonText(generated)))
					parameters, manual := mergeDocumentedParameters(array(current["parameters"]), array(base["parameters"]), generated)
					newKind := current["type"]
					if current["type"] == base["type"] {
						newKind = kind
					}
					if table == "models" {
						// Existing price conditions must remain representable after the capability update.
						for _, price := range s.All("SELECT conditions FROM prices WHERE model_id=?", current["id"]) {
							for name, value := range object(price["conditions"]) {
								if !documentedConditionExists(parameters, name, value) {
									parameters = array(current["parameters"])
									newKind = current["type"]
									manual = append(manual, "parameters used by a price rule")
									break
								}
							}
						}
					}
					description := str(current["description"])
					if description == "" || description == str(base["description"]) {
						description = documentedModelDescription(vendor, str(current["upstream_model"]), str(newKind), parameters)
					} else if description != documentedModelDescription(vendor, str(current["upstream_model"]), str(newKind), parameters) {
						manual = append(manual, "description")
					}
					if len(manual) > 0 {
						preserved = append(preserved, Row{"table": table, "id": current["id"], "fields": manual})
					}
					if description == str(current["description"]) && newKind == current["type"] && jsonText(parameters) == jsonText(array(current["parameters"])) {
						continue
					}
					changes = append(changes, Row{"table": table, "id": current["id"], "vendor": vendor, "upstream": current["upstream_model"], "before": Row{"description": current["description"], "type": current["type"], "parameters": array(current["parameters"])}, "after": Row{"description": description, "type": newKind, "parameters": parameters}})
					counts[table] = integer(counts[table]) + 1
				}
			}
			if apply && len(changes) > 0 {
				backup := Row{"models": s.All("SELECT * FROM models ORDER BY id"), "providerModels": s.All("SELECT * FROM provider_models ORDER BY id"), "prices": s.All("SELECT * FROM prices ORDER BY id")}
				backupDir := filepath.Join(a.Config.DataDir, "backups")
				must(os.MkdirAll(backupDir, 0700))
				backupPath := filepath.Join(backupDir, "model-documentation-"+id()+".encrypted.json")
				must(os.WriteFile(backupPath, []byte(a.encrypt(jsonText(backup))), 0600))
				counts["backup"] = backupPath
				for _, change := range changes {
					after := object(change["after"])
					s.Exec("UPDATE "+str(change["table"])+" SET description=?,type=?,parameters=? WHERE id=?", after["description"], after["type"], jsonText(after["parameters"]), change["id"])
				}
				if jsonText(s.All("SELECT * FROM prices ORDER BY id")) != jsonText(backup["prices"]) {
					panic("unexpected price change")
				}
				admin := s.One("SELECT id FROM users WHERE role='admin' ORDER BY created_at LIMIT 1")
				if admin != nil {
					s.Audit(str(admin["id"]), "maintenance.model_documentation", counts)
				}
			}
		})
	})
	if err != nil {
		return nil, err
	}
	result := Row{"applied": apply, "counts": counts, "changes": changes, "preservedCustomFields": preserved, "sourceDate": vendorDocumentation["verifiedAt"]}
	return result, nil
}

func mergeDocumentedParameters(current, baseline, desired []any) ([]any, []string) {
	byName := func(rows []any) map[string]Row {
		result := map[string]Row{}
		for _, raw := range rows {
			p := object(raw)
			result[str(p["name"])] = p
		}
		return result
	}
	before, newDefs := byName(baseline), byName(desired)
	present := map[string]bool{}
	result := []any{}
	manual := []string{}
	for _, raw := range current {
		p := object(raw)
		name := str(p["name"])
		present[name] = true
		if next := newDefs[name]; next != nil && sameDocumentedParameter(p, next) {
			result = append(result, p)
			continue
		}
		if old := before[name]; old != nil && sameDocumentedParameter(p, old) {
			if next := newDefs[name]; next != nil {
				result = append(result, next)
			}
		} else {
			result = append(result, p)
			if next := newDefs[name]; next != nil && !sameDocumentedParameter(next, p) {
				manual = append(manual, name)
			}
		}
	}
	for _, raw := range desired {
		p := object(raw)
		name := str(p["name"])
		if !present[name] && before[name] == nil {
			result = append(result, p)
		}
	}
	return result, manual
}

func sameDocumentedParameter(left, right Row) bool {
	normalize := func(p Row) Row {
		p = clone(p)
		if p["type"] == "select" {
			options := append([]any{}, array(p["options"])...)
			sort.Slice(options, func(i, j int) bool { return jsonText(options[i]) < jsonText(options[j]) })
			p["options"] = options
		}
		return p
	}
	return jsonText(normalize(left)) == jsonText(normalize(right))
}
func documentedConditionExists(parameters []any, name string, value any) bool {
	for _, raw := range parameters {
		p := object(raw)
		if p["name"] != name {
			continue
		}
		if p["type"] != "select" {
			return true
		}
		for _, option := range array(p["options"]) {
			v := option
			if o, ok := option.(map[string]any); ok {
				v = o["v"]
			}
			if str(v) == str(value) {
				return true
			}
		}
		return false
	}
	return false
}

func DocumentationSummary(result Row) string {
	return fmt.Sprintf("applied=%v counts=%s preservedCustomRows=%d", result["applied"], jsonText(result["counts"]), len(array(result["preservedCustomFields"])))
}

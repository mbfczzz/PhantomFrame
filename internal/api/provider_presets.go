package api

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// Nonsecret metadata from the user's supplier inventory. No API key field is
// included in this artifact; credentials are entered or imported at runtime.
//
//go:embed provider_presets.json
var providerPresetJSON []byte

var providerPresetRoutes = func() []InventoryRoute {
	var rows []InventoryRoute
	must(json.Unmarshal(providerPresetJSON, &rows))
	for i := range rows {
		r := &rows[i]
		if r.Key != "" || !inventoryVendorKnown(r.Vendor) || !generationKind(r.Modality) {
			panic("invalid built-in supplier metadata")
		}
		if r.Upstream == "" {
			r.Upstream = inventoryDefaultModel(r.Vendor, r.Model, object(r.Config["modelMap"]))
		}
	}
	return rows
}()

func inventoryDeletedProviders(s Store) map[string]bool {
	deleted := map[string]bool{}
	for _, row := range s.All("SELECT provider_id FROM inventory_deleted_providers") {
		deleted[str(row["provider_id"])] = true
	}
	return deleted
}

// Add missing templates only. Repeated starts never overwrite reviewed routes,
// credentials, parameter schemas, prices or enablement state.
func (a *App) ensureProviderPresets() {
	a.Tx(func(s Store) {
		s.Exec("INSERT INTO invariant_locks(name) VALUES('provider_inventory') ON DUPLICATE KEY UPDATE name='provider_inventory'")
		deleted := inventoryDeletedProviders(s)
		existing := map[string]Row{}
		for _, row := range s.All("SELECT * FROM inventory_routes") {
			existing[str(row["id"])] = row
		}
		bound := map[string]bool{}
		for _, row := range s.All("SELECT inventory_route_id FROM models WHERE inventory_route_id IS NOT NULL") {
			bound[str(row["inventory_route_id"])] = true
		}
		missing := []InventoryRoute{}
		for _, r := range providerPresetRoutes {
			if deleted[inventoryProviderID(r.Vendor)] {
				continue
			}
			if row := existing[r.ID]; row == nil {
				missing = append(missing, r)
			} else if !bound[r.ID] {
				// Repair a partial database using the reviewed route, never stale
				// built-in endpoints, mappings or parameters.
				missing = append(missing, inventoryRouteFromRow(row))
			}
		}
		if len(missing) == 0 {
			return
		}
		sources := inventoryPriceSources(s)
		for _, r := range missing {
			pid, mid := inventoryProviderID(r.Vendor), inventoryModelID(r.ID)
			if row := existing[r.ID]; row != nil {
				pid = str(row["provider_id"])
			}
			s.Exec("INSERT IGNORE INTO providers(id,name,protocol,base_url,api_key,enabled,config) VALUES(?,?,'inventory',?,'',1,'{}')", pid, r.Vendor, r.Base)
			provider := s.One("SELECT protocol FROM providers WHERE id=?", pid)
			if provider == nil || provider["protocol"] != "inventory" {
				panic(fmt.Errorf("供应商预设标识与已有渠道冲突"))
			}
			if s.One("SELECT id FROM models WHERE id=?", mid) != nil {
				panic(fmt.Errorf("供应商预设模型标识与已有模型冲突"))
			}
			if existing[r.ID] == nil {
				s.Exec("INSERT INTO inventory_routes(id,provider_id,vendor,model,upstream_model,modality,base_url,api_key,source_enabled,is_primary,priority,weight,config) VALUES(?,?,?,?,?,?,?,'',?,?,?,?,?)", r.ID, pid, r.Vendor, r.Model, r.Upstream, r.Modality, r.Base, r.Enabled, r.Primary, r.Priority, r.Weight, jsonText(r.Config))
			}
			name := []rune(r.Vendor + " · " + r.Model)
			if len(name) > 120 {
				name = name[:120]
			}
			s.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,category,enabled,tags,parameters,inventory_route_id) VALUES(?,?,?,?,?,?,?,0,?,?,?)", mid, string(name), r.Modality, pid, r.Upstream, "内置供应商路由；填写独立密钥并核对售价后启用", r.Vendor, jsonText([]string{r.Modality}), jsonText(inventoryParameters(r)), r.ID)
			inheritInventoryPrices(s, r, sources)
		}
	})
}

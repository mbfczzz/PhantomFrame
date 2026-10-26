package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

type InventoryRoute struct {
	ID, Vendor, Model, Upstream, Modality, Base, Key string
	Enabled, Primary                                 bool
	Priority, Weight                                 int
	Config                                           Row
}

func inventoryModelID(routeID string) string { return "inventory-" + hash(routeID)[:28] }
func inventoryProviderID(vendor string) string {
	return "inventory-" + hash(strings.ToLower(vendor))[:24]
}
func generationKind(kind string) bool {
	return kind == "image" || kind == "video" || kind == "text" || kind == "audio" || kind == "upscale"
}

// CSV is secret input: error messages identify a line/column, never its value.
func ReadProviderInventory(input io.Reader) ([]InventoryRoute, Row, error) {
	data, err := io.ReadAll(io.LimitReader(input, 8*1024*1024+1))
	if err != nil {
		return nil, nil, fmt.Errorf("无法读取供应商清单")
	}
	if len(data) > 8*1024*1024 || !utf8.Valid(data) {
		return nil, nil, fmt.Errorf("清单必须是小于 8MB 的 UTF-8 CSV")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff")))
	headers, err := reader.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("CSV 表头格式不正确")
	}
	columns := map[string]int{}
	for i, name := range headers {
		if _, ok := columns[name]; ok {
			return nil, nil, fmt.Errorf("CSV 含重复列")
		}
		columns[name] = i
	}
	for _, name := range []string{"route_id", "provider", "model", "modality", "base_url", "api_key", "enabled", "upstream_model", "model_map"} {
		if _, ok := columns[name]; !ok {
			return nil, nil, fmt.Errorf("CSV 缺少必要列 %s", name)
		}
	}
	routes := []InventoryRoute{}
	seen := map[string]bool{}
	providers := map[string]bool{}
	skipped := 0
	warnings := []Row{}
	for line := 2; ; line++ {
		cells, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, nil, fmt.Errorf("CSV 第 %d 行格式不正确", line)
		}
		get := func(name string) string {
			if n, ok := columns[name]; ok && n < len(cells) {
				return strings.TrimSpace(cells[n])
			}
			return ""
		}
		if line > 10001 {
			return nil, nil, fmt.Errorf("清单最多 10000 条路由")
		}
		r := InventoryRoute{ID: get("route_id"), Vendor: get("provider"), Model: get("model"), Upstream: get("upstream_model"), Modality: get("modality"), Base: strings.TrimRight(get("base_url"), "/"), Key: get("api_key"), Config: Row{}}
		if r.Modality == "3d" {
			skipped++
			continue
		}
		if r.Modality == "" && strings.EqualFold(r.Vendor, "dsas") {
			r.Modality = "video"
		}
		if r.ID == "" || len(r.ID) > 100 || r.Vendor == "" || len(r.Vendor) > 80 || r.Model == "" || len(r.Model) > 160 || len(r.Upstream) > 160 || !generationKind(r.Modality) || len(r.Key) > 4096 || len(r.Base) > 500 {
			return nil, nil, fmt.Errorf("CSV 第 %d 行标识、类型或长度不正确", line)
		}
		if seen[r.ID] {
			return nil, nil, fmt.Errorf("CSV 第 %d 行含重复路由编号", line)
		}
		seen[r.ID] = true
		if !inventoryVendorKnown(r.Vendor) {
			return nil, nil, fmt.Errorf("CSV 第 %d 行供应商尚未适配", line)
		}
		for _, field := range []struct {
			name   string
			target *bool
		}{{"enabled", &r.Enabled}, {"is_primary", &r.Primary}} {
			value := get(field.name)
			if value == "1" || strings.EqualFold(value, "true") {
				*field.target = true
			} else if value != "0" && value != "" && !strings.EqualFold(value, "false") {
				return nil, nil, fmt.Errorf("CSV 第 %d 行 %s 格式不正确", line, field.name)
			}
		}
		for _, field := range []struct {
			name   string
			target *int
		}{{"priority", &r.Priority}, {"weight", &r.Weight}} {
			value := get(field.name)
			if value != "" {
				n, e := strconv.Atoi(value)
				if e != nil || n < 0 || n > 1000000 {
					return nil, nil, fmt.Errorf("CSV 第 %d 行 %s 超出范围", line, field.name)
				}
				*field.target = n
			}
		}
		modelMap := Row{}
		if value := get("model_map"); value != "" {
			if json.Unmarshal([]byte(value), &modelMap) != nil {
				return nil, nil, fmt.Errorf("CSV 第 %d 行模型映射格式不正确", line)
			}
		}
		for _, value := range modelMap {
			if _, ok := value.(string); !ok {
				return nil, nil, fmt.Errorf("CSV 第 %d 行模型映射值必须是文本", line)
			}
		}
		r.Config = Row{"vendor": r.Vendor, "sourceModel": r.Model, "operation": get("operation"), "chatAPI": get("chat_api"), "modelMap": modelMap}
		for _, field := range []string{"submit_path_override", "poll_path_override"} {
			if value := get(field); value != "" {
				if !inventoryPathValid(value) {
					return nil, nil, fmt.Errorf("CSV 第 %d 行接口路径不正确", line)
				}
				r.Config[field] = value
			}
		}
		if r.Base != "" {
			u, e := url.Parse(r.Base)
			if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
				return nil, nil, fmt.Errorf("CSV 第 %d 行地址不正确或包含敏感 URL 参数", line)
			}
		}
		if r.Upstream == "" {
			r.Upstream = inventoryDefaultModel(r.Vendor, r.Model, modelMap)
		}
		if r.Base == "" || !inventoryCredentialReady(r.Key) {
			warnings = append(warnings, Row{"line": line, "routeId": r.ID, "reason": "缺少地址或有效密钥，保持不可调用"})
		}
		routes = append(routes, r)
		providers[strings.ToLower(r.Vendor)] = true
	}
	return routes, Row{"routes": len(routes), "providers": len(providers), "excluded3D": skipped, "warnings": warnings}, nil
}

func (a *App) ImportProviderInventory(input io.Reader, apply bool) (Row, error) {
	return a.importProviderInventory(input, apply, false)
}

func (a *App) importProviderInventory(input io.Reader, apply, startup bool) (Row, error) {
	routes, report, err := ReadProviderInventory(input)
	if err != nil {
		return nil, err
	}
	report["applied"] = apply
	if !apply {
		return report, nil
	}
	err = try(func() {
		a.Tx(func(s Store) {
			s.Exec("INSERT IGNORE INTO invariant_locks(name) VALUES('provider_inventory')")
			s.One("SELECT name FROM invariant_locks WHERE name='provider_inventory' FOR UPDATE")
			fingerprint := hash(jsonText(routes))
			if startup {
				previous := s.One("SELECT value FROM settings WHERE `key`='_inventoryStartupFingerprint'")
				if previous != nil && str(previous["value"]) == fingerprint {
					report["applied"], report["unchanged"] = false, true
					return
				}
			}
			sources := inventoryPriceSources(s)
			inherited, preserved := 0, 0
			pending := []Row{}
			for _, r := range routes {
				pid := inventoryProviderID(r.Vendor)
				s.Exec("INSERT IGNORE INTO providers(id,name,protocol,base_url,api_key,enabled,config) VALUES(?,?,'inventory',?,'',1,'{}')", pid, r.Vendor, r.Base)
				old := s.One("SELECT api_key FROM inventory_routes WHERE id=? FOR UPDATE", r.ID)
				encrypted := a.encrypt(r.Key)
				if old != nil {
					if previous, e := a.decrypt(str(old["api_key"])); e == nil && previous == r.Key {
						encrypted = str(old["api_key"])
					}
				}
				s.Exec("INSERT INTO inventory_routes(id,provider_id,vendor,model,upstream_model,modality,base_url,api_key,source_enabled,is_primary,priority,weight,config) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE provider_id=VALUES(provider_id),vendor=VALUES(vendor),model=VALUES(model),upstream_model=VALUES(upstream_model),modality=VALUES(modality),base_url=VALUES(base_url),api_key=VALUES(api_key),source_enabled=VALUES(source_enabled),is_primary=VALUES(is_primary),priority=VALUES(priority),weight=VALUES(weight),config=VALUES(config),updated_at=CURRENT_TIMESTAMP(3)", r.ID, pid, r.Vendor, r.Model, r.Upstream, r.Modality, r.Base, encrypted, r.Enabled, r.Primary, r.Priority, r.Weight, jsonText(r.Config))
				mid := inventoryModelID(r.ID)
				name := []rune(r.Vendor + " · " + r.Model)
				if len(name) > 120 {
					name = name[:120]
				}
				s.Exec("INSERT IGNORE INTO models(id,name,type,provider_id,upstream_model,description,category,enabled,tags,parameters,inventory_route_id) VALUES(?,?,?,?,?,?,?,0,?,?,?)", mid, string(name), r.Modality, pid, r.Upstream, "供应商清单导入；配置售价并确认路由后启用", r.Vendor, jsonText([]string{r.Modality}), jsonText(inventoryParameters(r)), r.ID)
				// Re-import never overwrites a reviewed price or parameter schema. A changed
				// binding requires renewed review, and a disabled/incomplete source stays off.
				s.Exec("UPDATE models SET enabled=0 WHERE id=? AND (provider_id<>? OR upstream_model<>? OR type<>? OR ?=0 OR ?='' OR ?=0)", mid, pid, r.Upstream, r.Modality, r.Enabled, r.Base, inventoryCredentialReady(r.Key))
				s.Exec("UPDATE models SET provider_id=?,upstream_model=?,type=? WHERE id=?", pid, r.Upstream, r.Modality, mid)
				switch outcome := inheritInventoryPrices(s, r, sources); outcome {
				case "inherited":
					inherited++
				case "preserved":
					preserved++
				default:
					pending = append(pending, Row{"routeId": r.ID, "reason": outcome})
				}
			}
			report["pricesInherited"], report["pricesPreserved"], report["pricesPending"] = inherited, preserved, pending
			if startup {
				s.Exec("INSERT INTO settings(`key`,value) VALUES('_inventoryStartupFingerprint',?) ON DUPLICATE KEY UPDATE value=VALUES(value)", fingerprint)
			}
			if admin := s.One("SELECT id FROM users WHERE role='admin' AND status='active' ORDER BY created_at LIMIT 1"); admin != nil {
				s.Audit(str(admin["id"]), "inventory.import", Row{"routes": len(routes), "providers": report["providers"], "excluded3D": report["excluded3D"]})
			}
		})
	})
	return report, err
}

func (a *App) ImportProviderInventoryFile(path string) (Row, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, fmt.Errorf("无法打开供应商清单文件")
	}
	defer f.Close()
	return a.importProviderInventory(f, true, true)
}

func (a *App) inventoryRoutes(m *http.ServeMux) {
	a.route(m, "PATCH /api/admin/provider-inventory/{id}", 3, func(r *Request) any {
		body := r.Body()
		a.Tx(func(s Store) {
			row := s.One("SELECT * FROM inventory_routes WHERE id=? FOR UPDATE", r.ID())
			if row == nil {
				fail(404, "路由不存在")
			}
			base := trimField(body, "baseURL", 0, 500, str(row["base_url"]))
			if base != "" && !validBaseURL(base) {
				fail(400, "供应商地址不正确")
			}
			key := textField(body, "apiKey", 0, 4096, "")
			encrypted := str(row["api_key"])
			if key != "" {
				if !inventoryCredentialReady(key) {
					fail(400, "请输入有效密钥")
				}
				encrypted = a.encrypt(key)
			}
			enabled := boolField(body, "sourceEnabled", truth(row["source_enabled"]))
			s.Exec("UPDATE inventory_routes SET base_url=?,api_key=?,source_enabled=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=?", strings.TrimRight(base, "/"), encrypted, enabled, r.ID())
			if !enabled {
				s.Exec("UPDATE models SET enabled=0 WHERE inventory_route_id=?", r.ID())
			}
			s.Audit(r.UID(), "inventory.route.update", Row{"routeId": r.ID(), "enabled": enabled, "keyChanged": key != ""})
		})
		return Row{"ok": true}
	})
	a.route(m, "POST /api/admin/provider-inventory/import", 3, func(r *Request) any {
		r.R.Body = http.MaxBytesReader(r.W, r.R.Body, 9*1024*1024)
		if e := r.R.ParseMultipartForm(9 * 1024 * 1024); e != nil {
			fail(400, "请上传有效 CSV 文件")
		}
		defer r.R.MultipartForm.RemoveAll()
		file, _, e := r.R.FormFile("file")
		if e != nil {
			fail(400, "缺少 CSV 文件")
		}
		defer file.Close()
		report, e := a.ImportProviderInventory(file, r.R.FormValue("apply") == "true")
		if e != nil {
			fail(400, e.Error())
		}
		return report
	})
	a.route(m, "GET /api/admin/provider-inventory", 3, func(r *Request) any {
		items := []Row{}
		for _, row := range a.Store.All("SELECT r.*,p.enabled provider_enabled,m.id model_id,m.enabled model_enabled,(SELECT count(*) FROM prices pr WHERE pr.model_id=m.id AND pr.enabled=1) price_count FROM inventory_routes r JOIN providers p ON p.id=r.provider_id LEFT JOIN models m ON m.inventory_route_id=r.id ORDER BY r.vendor,r.model,r.id") {
			key, err := a.decrypt(str(row["api_key"]))
			ready := err == nil && inventoryCredentialReady(key)

			items = append(items, Row{"providerEnabled": truth(row["provider_enabled"]), "id": row["id"], "vendor": row["vendor"], "model": row["model"], "upstreamModel": row["upstream_model"], "type": row["modality"], "baseURL": row["base_url"], "hasKey": ready, "hasPrices": integer(row["price_count"]) > 0, "sourceEnabled": truth(row["source_enabled"]), "enabled": truth(row["model_enabled"]), "modelId": row["model_id"], "priority": row["priority"], "weight": row["weight"]})
		}
		return items
	})
}

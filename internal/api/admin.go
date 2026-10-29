package api

import (
	"context"
	"encoding/base64"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func providerView(p Row) Row {
	return Row{"id": p["id"], "name": p["name"], "protocol": p["protocol"], "base_url": p["base_url"], "hasKey": str(p["api_key"]) != "", "enabled": truth(p["enabled"]), "config": object(p["config"]), "createdAt": p["created_at"], "supportedCount": num(p["supported_count"]), "publishedCount": num(p["published_count"]), "modelsPath": providerModelsPath(p), "types": providerProtocol(str(p["protocol"]))["types"]}
}
func validBaseURL(s string) bool {
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func generationProviderConfig(config any) Row {
	result := clone(object(config))
	// Catalog location, display preference and poll cadence do not change the
	// generation endpoint/model mapping reviewed with the retail price.
	for _, name := range []string{"modelsPath", "preferred", "pollInterval"} {
		delete(result, name)
	}
	return result
}

func (a *App) saveProvider(r *Request) any {
	p := r.Body()
	name := trimField(p, "name", 1, 80)
	protocol := textField(p, "protocol", 1, 32)
	if providerProtocol(protocol) == nil && !(protocol == "inventory" && !a.Config.SupplierInstances) {
		fail(400, "请选择支持的接入协议")
	}
	base := textField(p, "base_url", 0, 500, "")
	key := strings.TrimSpace(textField(p, "apiKey", 0, 4096, ""))
	validateProviderKey(protocol, key)
	enabled := boolField(p, "enabled", true)
	config := mapField(p, "config")
	if path, ok := config["modelsPath"]; ok && str(path) != "" && !inventoryPathValid(str(path)) {
		fail(400, "模型列表接口路径不正确")
	}
	if nativeVendor(protocol) != "" {
		for name, value := range validateNativeModelConfig(config) {
			config[name] = value
		}
	}
	if protocol != "demo" && !(protocol == "inventory" && base == "") && !validBaseURL(base) {
		fail(400, "请输入有效的 API 基础地址")
	}
	if protocol == "demo" && !a.Config.Demo && enabled {
		fail(400, "演示模式已关闭")
	}
	update := r.R.Method == "PUT"
	pid := r.ID()
	if !update {
		pid = id()
	}
	a.Tx(func(s Store) {
		old := s.One("SELECT * FROM providers WHERE id=? FOR UPDATE", pid)
		if a.Config.SupplierInstances && old != nil && old["protocol"] != protocol {
			fail(409, "供应商实例的协议创建后不能修改，请新增供应商")
		}
		if old != nil && old["protocol"] == "inventory" && (protocol != "inventory" || key != "") {
			fail(400, "清单渠道的协议与密钥由独立路由管理，请前往供应商路由页")
		}
		if update && old == nil {
			fail(404, "供应商不存在")
		}
		if old != nil && old["protocol"] != protocol && s.One("SELECT id FROM models WHERE provider_id=? LIMIT 1", pid) != nil {
			fail(409, "已有发布模型的供应商不能更换协议，请新建供应商实例")
		}
		encrypted := ""
		if old != nil {
			encrypted = str(old["api_key"])
		}
		if key != "" {
			encrypted = a.encrypt(key)
		}
		if update {
			s.Exec("UPDATE providers SET name=?,protocol=?,base_url=?,api_key=?,enabled=?,config=? WHERE id=?", name, protocol, base, encrypted, enabled, jsonText(config), pid)
			if (a.Config.SupplierInstances || nativeVendor(protocol) != "") &&
				(strings.TrimRight(base, "/") != strings.TrimRight(str(old["base_url"]), "/") || jsonText(generationProviderConfig(config)) != jsonText(generationProviderConfig(old["config"]))) {
				s.Exec("UPDATE models SET enabled=0 WHERE provider_id=?", pid)
			}
		} else {
			s.Exec("INSERT INTO providers(id,name,protocol,base_url,api_key,enabled,config) VALUES(?,?,?,?,?,?,?)", pid, name, protocol, base, encrypted, enabled, jsonText(config))
		}
		action := "provider.create"
		if update {
			action = "provider.update"
		}
		s.Audit(r.UID(), action, Row{"id": pid, "name": name})
	})
	if !update {
		r.Status = 201
	}
	return providerView(a.Store.One("SELECT * FROM providers WHERE id=?", pid))
}
func validateParameters(v []any) []any {
	seen := map[string]bool{}
	for _, raw := range v {
		p, ok := raw.(map[string]any)
		if !ok {
			fail(400, "参数定义格式不正确")
		}
		name := textField(p, "name", 1, 100)
		if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`).MatchString(name) || name == "constructor" || name == "prototype" || name == "__proto__" || seen[name] {
			fail(400, "参数名称不正确或重复")
		}
		seen[name] = true
		textField(p, "label", 1, 80)
		kind := enumField(p, "type", "select", "text", "number", "boolean", "assets")
		if _, ok := p["default"]; ok && !isScalar(p["default"]) {
			fail(400, "参数默认值不正确")
		}
		if _, ok := p["required"]; ok {
			boolField(p, "required", false)
		}
		if _, ok := p["integer"]; ok {
			boolField(p, "integer", false)
		}
		if _, ok := p["itemType"]; ok {
			enumField(p, "itemType", "image", "video", "audio")
		}
		for _, k := range []string{"min", "max"} {
			if value, ok := p[k]; ok {
				if value == nil || value == "" {
					delete(p, k)
					continue
				}
				bound := numberField(p, k, -1e9, 1e9)
				if kind == "assets" && (bound < 0 || bound != math.Trunc(bound)) {
					fail(400, str(p["label"])+"的素材数量必须是非负整数，留空表示不限制")
				}
			}
		}
		if kind == "assets" {
			if p["maxTotal"] != nil {
				n := numberField(p, "maxTotal", 0, 1000000)
				if n != math.Trunc(n) {
					fail(400, "素材合计上限必须为整数")
				}
			}
			if p["maxByResolution"] != nil {
				for key := range mapField(p, "maxByResolution") {
					n := numberField(object(p["maxByResolution"]), key, 0, 1000000)
					if n != math.Trunc(n) {
						fail(400, "分辨率对应的素材上限必须为整数")
					}
				}
			}
		}
		if p["min"] != nil && p["max"] != nil && num(p["min"]) > num(p["max"]) {
			fail(400, str(p["label"])+"的最小值不能大于最大值")
		}
		if kind == "assets" && truth(p["required"]) && p["max"] != nil && num(p["max"]) == 0 {
			fail(400, str(p["label"])+"为必填素材，数量上限不能为 0")
		}
		options := listField(p, "options", 200)
		if kind == "select" && len(options) == 0 {
			fail(400, str(p["label"])+"缺少选项")
		}
		for _, o := range options {
			if isScalar(o) {
				continue
			}
			m, ok := o.(map[string]any)
			if !ok || !isScalar(m["v"]) {
				fail(400, "参数选项格式不正确")
			}
			textField(m, "l", 0, 200)
		}
	}
	return v
}
func isScalar(v any) bool {
	switch x := v.(type) {
	case string, bool:
		return true
	case float64:
		return !math.IsNaN(x) && !math.IsInf(x, 0)
	}
	return false
}
func (a *App) saveModel(r *Request) any {
	p := r.Body()
	mid := textField(p, "id", 1, 100)
	if !regexp.MustCompile(`^[a-zA-Z0-9_.:-]+$`).MatchString(mid) {
		fail(400, "模型 ID 格式不正确")
	}
	name := textField(p, "name", 1, 120)
	kind := enumField(p, "type", "image", "video", "text", "audio", "upscale")
	pid := textField(p, "provider_id", 1, 100)
	upstream := textField(p, "upstream_model", 1, 160)
	desc := textField(p, "description", 0, 2000, "")
	category := textField(p, "category", 0, 80, "默认分组")
	enabled := boolField(p, "enabled", true)
	order := numberField(p, "sort_order", 0, 100000, 0)
	if order != math.Trunc(order) {
		fail(400, "排序必须是整数")
	}
	tags := listField(p, "tags", 10)
	for _, t := range tags {
		textField(Row{"tag": t}, "tag", 0, 30)
	}
	params := imageRequestParameters(kind, validateParameters(listField(p, "parameters", 40)))
	desc = imageRequestDescription(kind, upstream, desc)
	update := r.R.Method == "PUT"
	if update && mid != r.ID() {
		fail(400, "模型 ID 创建后不能修改")
	}
	a.Tx(func(s Store) {
		provider := s.One("SELECT id,protocol FROM providers WHERE id=? FOR UPDATE", pid)
		if provider == nil {
			fail(400, "供应商不存在")
		}
		if provider["protocol"] == "media-upload" {
			fail(400, "素材上传服务不能绑定生成模型")
		}
		validateProviderModelKind(provider, kind)
		old := s.One("SELECT * FROM models WHERE id=? FOR UPDATE", mid)
		if old != nil && str(old["inventory_route_id"]) != "" && (pid != str(old["provider_id"]) || upstream != str(old["upstream_model"]) || kind != str(old["type"])) {
			fail(409, "清单模型的路由绑定不能直接修改，请更新供应商清单后重新导入")
		}
		if !update && provider["protocol"] == "inventory" {
			fail(400, "清单渠道请通过 CSV 新增独立路由")
		}
		if update && old == nil {
			fail(404, "模型不存在")
		}
		if !update && old != nil {
			fail(409, "模型 ID 已存在")
		}
		if s.One("SELECT id FROM providers WHERE id=?", pid) == nil {
			fail(400, "供应商不存在")
		}
		if update {
			s.Exec("UPDATE models SET name=?,type=?,provider_id=?,upstream_model=?,description=?,category=?,enabled=?,sort_order=?,tags=?,parameters=? WHERE id=?", name, kind, pid, upstream, desc, category, enabled, order, jsonText(tags), jsonText(params), mid)
		} else {
			s.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,category,enabled,sort_order,tags,parameters) VALUES(?,?,?,?,?,?,?,?,?,?,?)", mid, name, kind, pid, upstream, desc, category, enabled, order, jsonText(tags), jsonText(params))
		}
		s.ensureProviderModel(s.One("SELECT * FROM models WHERE id=?", mid))
		action := "model.create"
		if update {
			action = "model.update"
		}
		s.Audit(r.UID(), action, Row{"id": mid})
	})
	if !update {
		r.Status = 201
	}
	return a.Store.ModelView(a.Store.One("SELECT * FROM models WHERE id=?", mid))
}
func (a *App) savePrice(r *Request) any {
	p := r.Body()
	mid := textField(p, "model_id", 1, 100)
	update := r.R.Method == "PUT"
	pid := r.ID()
	if !update {
		pid = id()
	}
	a.Tx(func(s Store) {
		m := s.One("SELECT * FROM models WHERE id=? FOR UPDATE", mid)
		if m == nil {
			fail(400, "模型不存在")
		}
		writePriceRule(s, m, pid, p, update)
		s.Audit(r.UID(), "price.save", Row{"id": pid, "model_id": mid, "credits": p["credits"], "conditions": p["conditions"]})
	})
	return priceView(a.Store.One("SELECT * FROM prices WHERE id=?", pid))
}
func (a *App) adminRoutes(m *http.ServeMux) {
	a.providerModelRoutes(m)
	a.route(m, "PUT /api/admin/models/{id}/prices", 3, a.saveModelPrices)
	a.route(m, "POST /api/admin/providers/{id}/test-upload", 3, func(r *Request) any {
		p := a.Store.One("SELECT * FROM providers WHERE id=?", r.ID())
		if p == nil || (p["protocol"] != "media-upload" && p["protocol"] != "suda" && p["protocol"] != "gway") {
			fail(400, "此渠道未提供独立的文件上传接口")
		}
		key, e := a.decrypt(str(p["api_key"]))
		if e != nil || key == "" {
			fail(400, "请先配置文件服务密钥")
		}
		data, e := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII=")
		must(e)
		file, e := os.CreateTemp(a.Config.FilesDir, "upload-check-*.png")
		must(e)
		defer os.Remove(file.Name())
		_, e = file.Write(data)
		file.Close()
		must(e)
		ctx, cancel := context.WithTimeout(r.R.Context(), 60*time.Second)
		defer cancel()
		address, e := a.vendorUpload(ctx, p, key, Row{"file": filepath.Base(file.Name()), "name": "connection-test.png", "mime": "image/png", "type": "image", "size": len(data)})
		if e != nil {
			fail(502, errorMessage(e))
		}
		content, e := downloadVendorOutput(ctx, address, "", "")
		if e != nil {
			fail(502, "文件已上传，但无法通过公网链接读取")
		}
		mt, _ := detectMime(content)
		if !strings.HasPrefix(mt, "image/") {
			fail(502, "上传链接没有返回图片")
		}
		u, _ := url.Parse(address)
		a.Store.Audit(r.UID(), "provider.upload_test", Row{"providerId": r.ID(), "bytes": len(data)})
		return Row{"ok": true, "message": "上传成功，公网图片链接可访问", "host": u.Host}
	})
	a.route(m, "GET /api/admin/stats", 3, func(r *Request) any {
		v := Row{"currency": currencyCode}
		for _, name := range []string{"users", "models", "jobs"} {
			v[name] = a.Store.One("SELECT count(*) n FROM " + name)["n"]
		}
		v["activeJobs"] = a.Store.One("SELECT count(*) n FROM jobs WHERE status IN ('queued','running','refund_pending')")["n"]
		v["consumed"] = credits(a.Store.One("SELECT coalesce(sum(-amount),0) n FROM transactions WHERE kind IN ('generation','refund')")["n"])
		v["providers"] = a.Store.One("SELECT count(*) n FROM providers WHERE enabled=1")["n"]
		v["daily"] = a.Store.All("SELECT date(created_at) date,count(*) count,sum(status='succeeded') succeeded,sum(CASE WHEN refunded=0 THEN cost ELSE 0 END)/100.0 credits FROM jobs WHERE created_at>=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 14 DAY) GROUP BY date(created_at)")
		recent := []Row{}
		for _, j := range a.Store.All("SELECT j.*,u.username FROM jobs j JOIN users u ON u.id=j.user_id ORDER BY j.seq DESC LIMIT 8") {
			p := a.jobView(a.Store, j)
			p["username"] = j["username"]
			recent = append(recent, p)
		}
		v["recent"] = recent
		return v
	})
	a.route(m, "GET /api/admin/providers", 3, func(r *Request) any {
		v := []Row{}
		for _, p := range a.Store.All("SELECT p.*,((SELECT count(*) FROM provider_models c WHERE c.provider_id=p.id)+(SELECT count(*) FROM inventory_routes r WHERE r.provider_id=p.id)) supported_count,(CASE WHEN p.protocol='inventory' THEN (SELECT count(*) FROM models im WHERE im.provider_id=p.id) ELSE (SELECT count(*) FROM provider_models c WHERE c.provider_id=p.id AND EXISTS(SELECT 1 FROM models m WHERE m.provider_id=c.provider_id AND m.upstream_model=c.upstream_model)) END) published_count FROM providers p ORDER BY p.created_at") {
			v = append(v, providerView(p))
		}
		return v
	})
	a.route(m, "POST /api/admin/providers", 3, a.saveProvider)
	a.route(m, "PUT /api/admin/providers/{id}", 3, a.saveProvider)
	a.route(m, "DELETE /api/admin/providers/{id}", 3, func(r *Request) any {
		a.Tx(func(s Store) {
			// Use the same lock and order as inventory startup/import so deletion
			// cannot race with template creation or leave a partially removed supplier.
			s.Exec("INSERT INTO invariant_locks(name) VALUES('provider_inventory') ON DUPLICATE KEY UPDATE name='provider_inventory'")
			provider := s.One("SELECT id,protocol FROM providers WHERE id=? FOR UPDATE", r.ID())
			if provider == nil {
				return
			}
			if provider["protocol"] == "inventory" || nativeVendor(provider["protocol"]) != "" {
				// Route editors take route locks before model locks too.
				routes := s.All("SELECT id FROM inventory_routes WHERE provider_id=? ORDER BY id FOR UPDATE", r.ID())
				models := s.All("SELECT id FROM models WHERE provider_id=? ORDER BY id FOR UPDATE", r.ID())
				if s.One("SELECT j.id FROM jobs j JOIN models m ON m.id=j.model_id WHERE m.provider_id=? LIMIT 1", r.ID()) != nil {
					fail(409, "此供应商有关联生成任务，不能删除；请停用渠道以保留历史和计费记录")
				}
				s.Exec("DELETE pr FROM prices pr JOIN models m ON m.id=pr.model_id WHERE m.provider_id=?", r.ID())
				s.Exec("DELETE FROM models WHERE provider_id=?", r.ID())
				s.Exec("DELETE FROM inventory_routes WHERE provider_id=?", r.ID())
				if provider["protocol"] == "inventory" {
					s.Exec("INSERT INTO inventory_deleted_providers(provider_id) VALUES(?) ON DUPLICATE KEY UPDATE deleted_at=CURRENT_TIMESTAMP(3)", r.ID())
				}
				s.Exec("DELETE FROM providers WHERE id=?", r.ID())
				s.Audit(r.UID(), "provider.delete", Row{"id": r.ID(), "routes": len(routes), "models": len(models)})
				return
			}
			if s.One("SELECT id FROM models WHERE provider_id=? LIMIT 1", r.ID()) != nil {
				fail(400, "渠道下仍有已发布模型，不能删除；可先停用渠道")
			}
			s.Exec("DELETE FROM providers WHERE id=?", r.ID())
			s.Audit(r.UID(), "provider.delete", Row{"id": r.ID()})
		})
		return Row{"ok": true}
	})
	a.route(m, "POST /api/admin/providers/{id}/test", 3, func(r *Request) any {
		p := a.Store.One("SELECT * FROM providers WHERE id=?", r.ID())
		if p == nil {
			fail(404, "供应商不存在")
		}
		if p["protocol"] == "demo" {
			return Row{"ok": a.Config.Demo, "message": "本地演示通道"}
		}
		if p["protocol"] == "inventory" {
			return Row{"ok": false, "message": "此渠道使用独立路由配置，请在供应商路由页检查地址、密钥与模型价格"}
		}
		ctx, cancel := context.WithTimeout(r.R.Context(), 15*time.Second)
		defer cancel()
		key, e := a.decrypt(str(p["api_key"]))
		if e != nil {
			return Row{"ok": false, "message": "通道密钥无法解密"}
		}
		path := providerModelsPath(p)
		if path == "" {
			return Row{"ok": false, "message": "此供应商未配置模型列表接口，可手动添加模型"}
		}
		address, e := inventoryEndpoint(str(p["base_url"]), path)
		if e != nil {
			return Row{"ok": false, "message": "API 地址或模型列表路径不正确"}
		}
		req, e := http.NewRequestWithContext(ctx, "GET", address, nil)
		if e != nil {
			return Row{"ok": false, "message": "API 地址格式不正确"}
		}
		header := "Authorization"
		if vendor := nativeVendor(p["protocol"]); vendor != "" {
			header = inventoryAuth(vendor)
		}
		if header == "Authorization" {
			req.Header.Set(header, "Bearer "+key)
		} else {
			req.Header.Set(header, key)
		}
		res, e := upstreamClient().Do(req)
		if e != nil {
			return Row{"ok": false, "message": "连接失败，请核对基础地址和密钥"}
		}
		defer res.Body.Close()
		_, _ = io.CopyN(io.Discard, res.Body, 4096)
		message := "连接成功"
		if res.StatusCode >= 400 {
			message = "连接返回 HTTP " + str(res.StatusCode) + "（部分视频供应商不提供 /models 接口）"
		}
		return Row{"ok": res.StatusCode < 400, "message": message}
	})
	a.route(m, "GET /api/admin/models", 3, func(r *Request) any {
		return a.Store.ModelViews(a.Store.All("SELECT * FROM models ORDER BY sort_order,name"))
	})
	a.route(m, "POST /api/admin/models", 3, a.saveModel)
	a.route(m, "PUT /api/admin/models/{id}", 3, a.saveModel)
	a.route(m, "DELETE /api/admin/models/{id}", 3, func(r *Request) any {
		a.Tx(func(s Store) {
			model := s.One("SELECT id,inventory_route_id FROM models WHERE id=? FOR UPDATE", r.ID())
			if model != nil && str(model["inventory_route_id"]) != "" {
				fail(400, "清单模型请使用停用功能，以保留独立路由配置")
			}
			if s.One("SELECT id FROM jobs WHERE model_id=? LIMIT 1", r.ID()) != nil {
				fail(400, "该模型有关联任务，请使用停用功能保留历史记录")
			}
			s.Exec("DELETE FROM prices WHERE model_id=?", r.ID())
			s.Exec("DELETE FROM models WHERE id=?", r.ID())
			s.Audit(r.UID(), "model.delete", Row{"id": r.ID()})
		})
		return Row{"ok": true}
	})
	a.route(m, "GET /api/admin/prices", 3, func(r *Request) any {
		v := a.Store.All("SELECT p.*,m.name modelName,m.type FROM prices p JOIN models m ON m.id=p.model_id ORDER BY m.sort_order,p.credits")
		for _, p := range v {
			priceView(p)
		}
		return v
	})
	a.route(m, "POST /api/admin/prices", 3, a.savePrice)
	a.route(m, "PUT /api/admin/prices/{id}", 3, a.savePrice)
	a.route(m, "DELETE /api/admin/prices/{id}", 3, func(r *Request) any {
		a.Tx(func(s Store) {
			p := s.One("SELECT model_id FROM prices WHERE id=?", r.ID())
			if p != nil {
				s.One("SELECT id FROM models WHERE id=? FOR UPDATE", p["model_id"])
			}
			s.Exec("DELETE FROM prices WHERE id=?", r.ID())
			s.Audit(r.UID(), "price.delete", Row{"id": r.ID()})
		})
		return Row{"ok": true}
	})
	a.route(m, "GET /api/admin/users", 3, func(r *Request) any {
		v := []Row{}
		for _, u := range a.Store.All("SELECT * FROM users ORDER BY created_at DESC") {
			v = append(v, publicUser(u))
		}
		return v
	})
	a.route(m, "PATCH /api/admin/users/{id}", 3, func(r *Request) any {
		p := r.Body()
		a.Tx(func(s Store) {
			s.One("SELECT name FROM invariant_locks WHERE name='administrators' FOR UPDATE")
			actor := s.One("SELECT role,status,auth_version FROM users WHERE id=? FOR UPDATE", r.UID())
			if actor == nil || actor["role"] != "admin" || actor["status"] != "active" || integer(actor["auth_version"]) != integer(r.User["auth_version"]) {
				fail(403, "管理员权限已变更，请刷新后重试")
			}
			u := s.One("SELECT * FROM users WHERE id=? FOR UPDATE", r.ID())
			if u == nil {
				fail(404, "用户不存在")
			}
			status, role := str(u["status"]), str(u["role"])
			if _, ok := p["status"]; ok {
				status = enumField(p, "status", "active", "disabled")
			}
			if _, ok := p["role"]; ok {
				role = enumField(p, "role", "admin", "user")
			}
			if r.ID() == r.UID() && (status == "disabled" || role == "user") {
				fail(400, "不能停用或降级当前管理员")
			}
			if u["role"] == "admin" && u["status"] == "active" && (role != "admin" || status != "active") && integer(s.One("SELECT count(*) n FROM users WHERE role='admin' AND status='active'")["n"]) <= 1 {
				fail(409, "必须保留至少一名启用的管理员")
			}
			s.Exec("UPDATE users SET status=?,role=?,auth_version=auth_version+? WHERE id=?", status, role, status != u["status"] || role != u["role"], r.ID())
			s.Audit(r.UID(), "user.update", Row{"id": r.ID(), "status": status, "role": role})
		})
		return Row{"ok": true}
	})
	a.route(m, "POST /api/admin/users/{id}/credits", 3, func(r *Request) any {
		p := r.Body()
		amount := units(moneyField(p, "amount", -1000000, 1000000))
		reason := trimField(p, "reason", 2, 200)
		return a.moneyOperation(r, Row{"amount": amount, "reason": reason}, func(s Store) Row {
			if s.One("SELECT id FROM users WHERE id=? FOR UPDATE", r.ID()) == nil {
				fail(404, "用户不存在")
			}
			transaction := s.Wallet(r.ID(), amount, "adjustment", reason, nil)
			s.Audit(r.UID(), "credits.adjust", Row{"userId": r.ID(), "amount": credits(amount), "reason": reason})
			return Row{"ok": true, "transactionId": transaction}
		})
	})
	a.route(m, "GET /api/admin/jobs", 3, a.listAdminJobs)
	a.route(m, "GET /api/admin/jobs/{id}", 3, a.adminJobDetail)
	a.route(m, "GET /api/admin/jobs/{id}/outputs/{assetID}", 3, a.adminJobOutput)
	a.route(m, "GET /api/admin/transactions", 3, a.listAdminTransactions)
	a.route(m, "GET /api/admin/codes", 3, a.listAdminCodes)
	a.route(m, "POST /api/admin/codes", 3, func(r *Request) any {
		p := r.Body()
		count := numberField(p, "count", 1, 100)
		if count != math.Trunc(count) {
			fail(400, "数量必须是整数")
		}
		value := moneyField(p, "credits", 0.01, 1000000)
		var expires any
		if p["expiresAt"] != nil {
			t, e := time.Parse(time.RFC3339, str(p["expiresAt"]))
			if e != nil {
				fail(400, "过期时间格式不正确")
			}
			expires = t.UTC()
		}
		codes := []string{}
		a.Tx(func(s Store) {
			for i := 0; i < int(count); i++ {
				code := "PF-" + strings.ToUpper(randomHex(10))
				s.Exec("INSERT INTO redeem_codes(id,code,credits,expires_at) VALUES(?,?,?,?)", id(), code, units(value), expires)
				codes = append(codes, code)
			}
			s.Audit(r.UID(), "codes.create", Row{"count": count, "credits": value})
		})
		r.Status = 201
		return codes
	})
	a.route(m, "DELETE /api/admin/codes/{id}", 3, func(r *Request) any {
		a.Tx(func(s Store) {
			p := s.One("SELECT used_by FROM redeem_codes WHERE id=? FOR UPDATE", r.ID())
			if p == nil {
				fail(404, "兑换码不存在")
			}
			if p["used_by"] != nil {
				fail(400, "已使用的兑换码不能删除")
			}
			s.Exec("DELETE FROM redeem_codes WHERE id=?", r.ID())
			s.Audit(r.UID(), "codes.delete", Row{"id": r.ID()})
		})
		return Row{"ok": true}
	})
	a.route(m, "GET /api/admin/settings", 3, func(r *Request) any { return a.Store.Settings() })
	a.route(m, "PUT /api/admin/settings", 3, func(r *Request) any {
		p := r.Body()
		v := Row{"siteName": textField(p, "siteName", 1, 40), "subtitle": textField(p, "subtitle", 0, 200), "registrationEnabled": enumField(p, "registrationEnabled", "true", "false"), "welcomeCredits": textField(p, "welcomeCredits", 1, 30), "support": textField(p, "support", 0, 200), "announcement": textField(p, "announcement", 0, 1000), "purchaseUrl": textField(p, "purchaseUrl", 0, 500)}
		n, err := strconv.ParseFloat(str(v["welcomeCredits"]), 64)
		if err != nil {
			fail(400, "新用户赠送金额格式不正确")
		}
		moneyField(Row{"amount": n}, "amount", 0, 1000000)
		if u := str(v["purchaseUrl"]); u != "" {
			parsed, e := url.Parse(u)
			if e != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
				fail(400, "充值地址格式不正确")
			}
		}
		a.Tx(func(s Store) {
			for k, value := range v {
				s.Exec("INSERT INTO settings(`key`,value) VALUES(?,?) ON DUPLICATE KEY UPDATE value=?", k, value, value)
			}
			s.Audit(r.UID(), "settings.update", v)
		})
		return v
	})
	a.route(m, "GET /api/admin/audit", 3, a.listAdminAudit)
}

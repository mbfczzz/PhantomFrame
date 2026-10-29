package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

func (s Store) ensureProviderModel(m Row) {
	if str(m["inventory_route_id"]) != "" {
		return
	}
	s.Exec("INSERT IGNORE INTO provider_models(id,provider_id,upstream_model,name,type,description,parameters,origin) VALUES(?,?,?,?,?,?,?,'existing')", id(), m["provider_id"], m["upstream_model"], m["name"], m["type"], str(m["description"]), jsonText(array(m["parameters"])))
}
func (a *App) backfillProviderModels() {
	a.Tx(func(s Store) { s.backfillProviderModels() })
}
func (s Store) backfillProviderModels() {
	for _, m := range s.All("SELECT m.* FROM models m WHERE (m.inventory_route_id IS NULL OR m.inventory_route_id='') AND NOT EXISTS(SELECT 1 FROM provider_models c WHERE c.provider_id=m.provider_id AND c.upstream_model=m.upstream_model) ORDER BY m.created_at,m.id") {
		s.ensureProviderModel(m)
	}
}
func (s Store) providerModelView(entry Row) Row {
	published := s.All("SELECT id,name,type,enabled FROM models WHERE provider_id=? AND upstream_model=? ORDER BY created_at,id", entry["provider_id"], entry["upstream_model"])
	return providerModelData(entry, published)
}
func providerModelData(entry Row, published []Row) Row {
	for _, model := range published {
		model["enabled"] = truth(model["enabled"])
	}
	kind := entry["type"]
	if len(published) > 0 {
		kind = published[0]["type"]
	}
	return Row{"id": entry["id"], "providerId": entry["provider_id"], "upstreamModel": entry["upstream_model"], "name": entry["name"], "type": kind, "description": entry["description"], "parameters": array(entry["parameters"]), "config": object(entry["config"]), "origin": entry["origin"], "hasKey": str(entry["api_key"]) != "", "publishedModels": published, "createdAt": entry["created_at"], "updatedAt": entry["updated_at"]}
}

// A credential belongs to the channel/upstream pair, including all published aliases.
// It is never included in catalog responses or audit details.
func (a *App) saveProviderModelKey(r *Request) any {
	p := r.Body()
	useDefault := boolField(p, "useDefault", false)
	key := strings.TrimSpace(textField(p, "apiKey", 0, 4096, ""))
	if useDefault && key != "" {
		fail(400, "恢复渠道默认密钥时请勿填写独立 API Key")
	}
	if !useDefault && key == "" {
		fail(400, "请输入此模型的独立 API Key")
	}
	var result Row
	a.Tx(func(s Store) {
		provider := s.One("SELECT id,protocol FROM providers WHERE id=? FOR UPDATE", r.ID())
		inventoryRejectLegacyCatalog(provider)
		validateProviderKey(provider["protocol"], key)
		entry := s.One("SELECT * FROM provider_models WHERE id=? AND provider_id=? FOR UPDATE", r.R.PathValue("entryID"), r.ID())
		if entry == nil {
			fail(404, "渠道模型不存在")
		}
		var encrypted any
		if !useDefault {
			encrypted = a.encrypt(key)
		}
		s.Exec("UPDATE provider_models SET api_key=?,updated_at=UTC_TIMESTAMP(3) WHERE id=?", encrypted, entry["id"])
		s.Audit(r.UID(), "provider_model.key.update", Row{"id": entry["id"], "providerId": r.ID(), "upstreamModel": entry["upstream_model"], "useDefault": useDefault})
		result = s.providerModelView(s.One("SELECT * FROM provider_models WHERE id=?", entry["id"]))
	})
	return result
}
func (a *App) listProviderModels(r *Request) any {
	if a.Store.One("SELECT id FROM providers WHERE id=?", r.ID()) == nil {
		fail(404, "渠道不存在")
	}
	page, limit := r.Page()
	where, args := []string{"c.provider_id=?"}, []any{r.ID()}
	exists := "EXISTS(SELECT 1 FROM models m WHERE m.provider_id=c.provider_id AND m.upstream_model=c.upstream_model)"
	switch adminQueryChoice(r, "status", "published", "unpublished") {
	case "published":
		where = append(where, exists)
	case "unpublished":
		where = append(where, "NOT "+exists)
	}
	adminQuerySearch(r, []string{"c.upstream_model", "c.name"}, &where, &args)
	clause := " FROM provider_models c WHERE " + strings.Join(where, " AND ")
	total := a.Store.One("SELECT count(*) n"+clause, args...)["n"]
	items := []Row{}
	entries := a.Store.All("SELECT c.*"+clause+" ORDER BY c.created_at DESC,c.id DESC LIMIT ? OFFSET ?", append(args, limit, (page-1)*limit)...)
	published := map[string][]Row{}
	if len(entries) > 0 {
		upstreams := []any{r.ID()}
		for _, entry := range entries {
			upstreams = append(upstreams, entry["upstream_model"])
		}
		for _, model := range a.Store.All("SELECT id,name,type,enabled,upstream_model FROM models WHERE provider_id=? AND upstream_model IN ("+sqlMarks(len(entries))+") ORDER BY created_at,id", upstreams...) {
			upstream := str(model["upstream_model"])
			delete(model, "upstream_model")
			published[upstream] = append(published[upstream], model)
		}
	}
	for _, entry := range entries {
		rows := published[str(entry["upstream_model"])]
		if rows == nil {
			rows = []Row{}
		}
		items = append(items, providerModelData(entry, rows))
	}
	return Row{"items": items, "total": total, "page": page, "limit": limit}
}
func providerModelType(p Row) string {
	if p["type"] == nil || p["type"] == "" {
		return ""
	}
	return enumField(p, "type", "image", "video", "text", "audio", "upscale")
}
func (a *App) saveProviderModel(r *Request) any {
	p := r.Body()
	upstream := trimField(p, "upstreamModel", 1, 160)
	defaultName := []rune(upstream)
	if len(defaultName) > 120 {
		defaultName = defaultName[:120]
	}
	name := trimField(p, "name", 1, 120, string(defaultName))
	kind := providerModelType(p)
	description := textField(p, "description", 0, 2000, "")
	parameters := imageRequestParameters(kind, validateParameters(listField(p, "parameters", 40)))
	description = imageRequestDescription(kind, upstream, description)
	entryID := r.R.PathValue("entryID")
	update := r.R.Method == "PUT"
	if !update {
		entryID = id()
	}
	var result Row
	a.Tx(func(s Store) {
		provider := s.One("SELECT * FROM providers WHERE id=? FOR UPDATE", r.ID())
		inventoryRejectLegacyCatalog(provider)
		if provider["protocol"] == "media-upload" {
			fail(400, "素材上传服务不能添加生成模型")
		}
		modelConfig := validateNativeModelConfig(mapField(p, "config"))
		if kind != "" || nativeVendor(provider["protocol"]) != "" {
			validateProviderModelKind(provider, kind)
		}
		if nativeVendor(provider["protocol"]) != "" {
			if boolField(p, "useProtocolDefaults", !update) {
				parameters = inventoryParameters(nativeModelRoute(provider, upstream, kind, modelConfig))
			}
		}
		if update {
			entry := s.One("SELECT * FROM provider_models WHERE id=? AND provider_id=? FOR UPDATE", entryID, r.ID())
			if entry == nil {
				fail(404, "渠道模型不存在")
			}
			if s.One("SELECT id FROM models WHERE provider_id=? AND upstream_model=? LIMIT 1", r.ID(), entry["upstream_model"]) != nil {
				fail(409, "该模型已经发布，请在模型管理中修改参数与价格")
			}
			s.Exec("UPDATE provider_models SET upstream_model=?,name=?,type=?,description=?,parameters=?,config=?,updated_at=UTC_TIMESTAMP(3) WHERE id=?", upstream, name, kind, description, jsonText(parameters), jsonText(modelConfig), entryID)
		} else {
			if s.One("SELECT id FROM provider_models WHERE provider_id=? AND upstream_model=?", r.ID(), upstream) != nil {
				fail(409, "此渠道已存在该上游模型")
			}
			s.Exec("INSERT INTO provider_models(id,provider_id,upstream_model,name,type,description,parameters,config) VALUES(?,?,?,?,?,?,?,?)", entryID, r.ID(), upstream, name, kind, description, jsonText(parameters), jsonText(modelConfig))
		}
		action := "provider_model.create"
		if update {
			action = "provider_model.update"
		}
		s.Audit(r.UID(), action, Row{"id": entryID, "providerId": r.ID(), "upstreamModel": upstream})
		result = s.providerModelView(s.One("SELECT * FROM provider_models WHERE id=?", entryID))
	})
	if !update {
		r.Status = 201
	}
	return result
}
func (a *App) deleteProviderModel(r *Request) any {
	a.Tx(func(s Store) {
		if s.One("SELECT id FROM providers WHERE id=? FOR UPDATE", r.ID()) == nil {
			fail(404, "渠道不存在")
		}
		entry := s.One("SELECT * FROM provider_models WHERE id=? AND provider_id=? FOR UPDATE", r.R.PathValue("entryID"), r.ID())
		if entry == nil {
			fail(404, "渠道模型不存在")
		}
		if s.One("SELECT id FROM models WHERE provider_id=? AND upstream_model=? LIMIT 1", r.ID(), entry["upstream_model"]) != nil {
			fail(409, "请先在模型管理中处理已发布模型")
		}
		s.Exec("DELETE FROM provider_models WHERE id=?", entry["id"])
		s.Audit(r.UID(), "provider_model.delete", Row{"id": entry["id"], "providerId": r.ID()})
	})
	return Row{"ok": true}
}
func (a *App) publishProviderModel(r *Request) any {
	p := r.Body()
	modelID := trimField(p, "modelId", 1, 100)
	if !regexp.MustCompile(`^[a-zA-Z0-9_.:-]+$`).MatchString(modelID) {
		fail(400, "站内模型 ID 格式不正确")
	}
	name := trimField(p, "name", 1, 120)
	kind := enumField(p, "type", "image", "video", "text", "audio", "upscale")
	category := textField(p, "category", 0, 80, "默认分组")
	var model Row
	alreadyPublished := false
	a.Tx(func(s Store) {
		provider := s.One("SELECT * FROM providers WHERE id=? FOR UPDATE", r.ID())
		if provider == nil {
			fail(404, "渠道不存在")
		}
		inventoryRejectLegacyCatalog(provider)
		if provider["protocol"] == "media-upload" {
			fail(400, "素材上传服务不能发布生成模型")
		}
		if !truth(provider["enabled"]) || (provider["protocol"] == "demo" && !a.Config.Demo) {
			fail(400, "请先启用此渠道")
		}
		entry := s.One("SELECT * FROM provider_models WHERE id=? AND provider_id=? FOR UPDATE", r.R.PathValue("entryID"), r.ID())
		if entry == nil {
			fail(404, "渠道模型不存在")
		}
		if existing := s.One("SELECT * FROM models WHERE provider_id=? AND upstream_model=? ORDER BY created_at,id LIMIT 1", r.ID(), entry["upstream_model"]); existing != nil {
			model = s.ModelView(existing)
			alreadyPublished = true
			return
		}
		if s.One("SELECT id FROM models WHERE id=? FOR UPDATE", modelID) != nil {
			fail(409, "站内模型 ID 已存在，请更换 ID")
		}
		validateProviderModelKind(provider, kind)
		parameters := imageRequestParameters(kind, validateParameters(array(entry["parameters"])))
		if nativeVendor(provider["protocol"]) != "" {
			if str(entry["type"]) != "" && entry["type"] != kind {
				fail(400, "发布类型须与渠道模型类型一致")
			}
			if len(parameters) == 0 {
				parameters = inventoryParameters(nativeModelRoute(provider, str(entry["upstream_model"]), kind, object(entry["config"])))
			}
		}
		var sources []inventoryPriceSource
		if provider["protocol"] == "opencrow" {
			definition := openCrowDefinition(str(entry["upstream_model"]))
			if definition == nil || definition["kind"] != kind {
				fail(400, "OpenCrow 模型类型与目录不一致")
			}
			sources = inventoryPriceSources(s)
		}
		s.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,category,enabled,sort_order,tags,parameters) VALUES(?,?,?,?,?,?,?,0,100,'[]',?)", modelID, name, kind, r.ID(), entry["upstream_model"], entry["description"], category, jsonText(parameters))
		if provider["protocol"] == "opencrow" {
			inheritModelPrices(s, modelID, str(entry["upstream_model"]), kind, sources)
		}
		s.Audit(r.UID(), "provider_model.publish", Row{"id": entry["id"], "providerId": r.ID(), "modelId": modelID})
		model = s.ModelView(s.One("SELECT * FROM models WHERE id=?", modelID))
	})
	if !alreadyPublished {
		r.Status = 201
	}
	return Row{"model": model, "alreadyPublished": alreadyPublished}
}
func (a *App) syncProviderModels(r *Request) any {
	provider := a.Store.One("SELECT * FROM providers WHERE id=?", r.ID())
	if provider == nil {
		fail(404, "渠道不存在")
	}
	inventoryRejectLegacyCatalog(provider)
	if provider["protocol"] == "demo" {
		fail(400, "演示渠道没有上游模型接口，请手动添加模型")
	}
	if provider["protocol"] == "media-upload" {
		fail(400, "素材上传服务不提供生成模型")
	}
	key, err := a.decrypt(str(provider["api_key"]))
	if err != nil {
		fail(400, "渠道密钥无法解密")
	}
	ctx, cancel := context.WithTimeout(r.R.Context(), 20*time.Second)
	defer cancel()
	path := providerModelsPath(provider)
	if path == "" {
		fail(400, "此协议未配置模型列表接口，请手动添加模型，或填写模型列表路径")
	}
	address, err := inventoryEndpoint(str(provider["base_url"]), path)
	if err != nil {
		fail(400, "供应商模型列表地址不正确")
	}
	leaseKey, leaseOwner := a.Config.RedisPrefix+"catalog-sync:"+r.ID(), token()
	acquired, err := a.Redis.SetNX(ctx, leaseKey, leaseOwner, 45*time.Second).Result()
	if err != nil {
		fail(503, "同步协调服务暂不可用，请稍后重试")
	}
	if !acquired {
		fail(409, "此供应商正在拉取模型，请稍后重试")
	}
	defer func() {
		release, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = a.Redis.Eval(release, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) else return 0 end`, []string{leaseKey}, leaseOwner).Err()
	}()
	request, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		fail(400, "渠道地址不正确")
	}
	if key != "" {
		header := "Authorization"
		if vendor := nativeVendor(provider["protocol"]); vendor != "" {
			header = inventoryAuth(vendor)
		}
		if header == "Authorization" {
			request.Header.Set(header, "Bearer "+key)
		} else {
			request.Header.Set(header, key)
		}
	}
	response, err := upstreamClient().Do(request)
	if err != nil {
		if ctx.Err() != nil {
			fail(504, "模型拉取超时或已取消，请重试")
		}
		fail(502, "读取渠道模型失败，请检查地址或手动添加")
	}
	body, err := boundedBody(response, 2*1024*1024)
	if err != nil {
		if ctx.Err() != nil {
			fail(504, "模型拉取超时或已取消，请重试")
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			hint := "请检查供应商接口状态"
			switch response.StatusCode {
			case http.StatusUnauthorized:
				hint = "鉴权失败，请检查此供应商的 API 密钥是否有效"
			case http.StatusForbidden:
				hint = "访问被拒绝，请检查账号权限或供应商访问限制"
			case http.StatusNotFound:
				hint = "模型列表路径不存在，请检查 API 基础地址和模型列表接口路径"
				if provider["protocol"] == "openvideo" {
					hint += "；OpenVideo 通常使用带 /v1 的基础地址和 /models 列表路径"
				}
			case http.StatusMethodNotAllowed:
				hint = "该路径不支持 GET 请求，请填写模型目录接口路径"
			case http.StatusTooManyRequests:
				hint = "上游请求频率受限，请稍后重试"
			default:
				if response.StatusCode >= 500 {
					hint = "上游服务暂时异常，请稍后重试"
				}
			}
			fail(502, fmt.Sprintf("上游模型接口返回 HTTP %d：%s", response.StatusCode, hint))
		}
		fail(502, "渠道模型接口返回异常，请检查连接或手动添加")
	}
	var decoded any
	if json.Unmarshal(body, &decoded) != nil {
		fail(502, "渠道模型列表不是有效 JSON")
	}
	values, ok := decoded.([]any)
	if !ok {
		root, _ := decoded.(map[string]any)
		values, ok = root["data"].([]any)
		if !ok {
			values, ok = root["models"].([]any)
		}
	}
	if !ok || len(values) > 5000 {
		fail(502, "渠道模型列表格式或数量不符合要求，请手动添加")
	}
	entries := []Row{}
	seen := map[string]bool{}
	for _, value := range values {
		item, _ := value.(map[string]any)
		upstream, ok := item["id"].(string)
		if !ok {
			upstream, ok = value.(string)
		}
		upstream = strings.TrimSpace(upstream)
		if !ok || upstream == "" || len([]rune(upstream)) > 160 || seen[upstream] {
			continue
		}
		name := str(first(item["display_name"], item["name"]))
		if name == "" {
			name = upstream
		}
		if len([]rune(name)) > 120 {
			name = string([]rune(name)[:120])
		}
		kind := strings.ToLower(strings.TrimSpace(str(first(item["type"], item["kind"], item["media_type"]))))
		if kind == "3d" || (generationKind(kind) && !providerSupportsKind(provider, kind)) {
			continue
		}
		if !generationKind(kind) {
			kind = ""
		}
		parameters := []any{}
		if vendor := nativeVendor(provider["protocol"]); vendor != "" {
			if kind == "" {
				for _, known := range providerPresetRoutes {
					if strings.EqualFold(known.Vendor, vendor) && known.Upstream == upstream {
						kind = known.Modality
						break
					}
				}
			}
			if generationKind(kind) {
				parameters = inventoryParameters(nativeModelRoute(provider, upstream, kind, Row{}))
			}
		}
		if isVendor(provider["protocol"]) {
			var supported bool
			kind, parameters, supported = vendorCatalogEntry(str(provider["protocol"]), item, object(decoded))
			if !supported {
				continue
			}
		}
		if kind != "" && !providerSupportsKind(provider, kind) {
			continue
		}
		seen[upstream] = true
		description := str(item["description"])
		if isVendor(provider["protocol"]) {
			description = documentedModelDescription(str(provider["protocol"]), upstream, kind, parameters)
		}
		entries = append(entries, Row{"upstream": upstream, "name": name, "type": kind, "parameters": parameters, "description": description})
	}
	if len(values) > 0 && len(entries) == 0 {
		fail(502, "渠道未返回可识别的模型 ID")
	}
	added := 0
	err = try(func() {
		a.TxContext(ctx, func(s Store) {
			current := s.One("SELECT * FROM providers WHERE id=? FOR UPDATE", r.ID())
			if current == nil {
				fail(404, "渠道不存在")
			}
			if current["base_url"] != provider["base_url"] || current["api_key"] != provider["api_key"] || current["protocol"] != provider["protocol"] || jsonText(object(current["config"])) != jsonText(object(provider["config"])) {
				fail(409, "渠道配置已变更，请重新拉取")
			}
			existing := map[string]bool{}
			for start := 0; start < len(entries); start += 200 {
				batch := entries[start:min(start+200, len(entries))]
				args := []any{r.ID()}
				for _, entry := range batch {
					args = append(args, entry["upstream"])
				}
				for _, entry := range s.All("SELECT upstream_model FROM provider_models WHERE provider_id=? AND upstream_model IN ("+sqlMarks(len(batch))+")", args...) {
					existing[str(entry["upstream_model"])] = true
				}
			}
			values, args := []string{}, []any{}
			flush := func() {
				if len(values) == 0 {
					return
				}
				s.Exec("INSERT INTO provider_models(id,provider_id,upstream_model,name,type,description,parameters,origin) VALUES "+strings.Join(values, ","), args...)
				values, args = []string{}, []any{}
			}
			for _, entry := range entries {
				if existing[str(entry["upstream"])] {
					continue
				}
				parameters := validateParameters(array(jsonText(entry["parameters"])))
				description := []rune(str(entry["description"]))
				if len(description) > 2000 {
					description = description[:2000]
				}
				values = append(values, "(?,?,?,?,?,?,?,'upstream')")
				args = append(args, id(), r.ID(), entry["upstream"], entry["name"], entry["type"], string(description), jsonText(parameters))
				added++
				if len(values) == 100 {
					flush()
				}
			}
			flush()
			s.Audit(r.UID(), "provider_models.sync", Row{"providerId": r.ID(), "received": len(entries), "added": added})
		})
	})
	if err != nil {
		if ctx.Err() != nil {
			fail(504, "模型拉取超时或已取消，请重试")
		}
		panic(err)
	}
	return Row{"received": len(entries), "added": added}
}
func (a *App) providerModelRoutes(m *http.ServeMux) {
	a.route(m, "GET /api/admin/providers/{id}/models", 3, a.listProviderModels)
	a.route(m, "POST /api/admin/providers/{id}/models", 3, a.saveProviderModel)
	a.route(m, "PUT /api/admin/providers/{id}/models/{entryID}", 3, a.saveProviderModel)
	a.route(m, "PUT /api/admin/providers/{id}/models/{entryID}/key", 3, a.saveProviderModelKey)
	a.route(m, "DELETE /api/admin/providers/{id}/models/{entryID}", 3, a.deleteProviderModel)
	a.route(m, "POST /api/admin/providers/{id}/models/{entryID}/publish", 3, a.publishProviderModel)
	a.route(m, "POST /api/admin/providers/{id}/sync-models", 3, a.syncProviderModels)
}

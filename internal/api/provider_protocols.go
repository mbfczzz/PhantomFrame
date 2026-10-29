package api

import (
	"net/http"
	"sort"
	"strings"
	"unicode"
)

func nativeVendor(protocol any) string {
	p := str(protocol)
	if strings.HasPrefix(p, "native:") && inventoryVendorKnown(strings.TrimPrefix(p, "native:")) {
		return strings.ToLower(strings.TrimPrefix(p, "native:"))
	}
	return ""
}

// Protocol metadata is not a supplier account or a model catalog. No rows or
// credentials are created from it; every account is explicitly administrator-owned.
func buildProviderProtocols() []Row {
	vendors := map[string]Row{}
	for _, route := range providerPresetRoutes {
		vendor := strings.ToLower(route.Vendor)
		if vendors[vendor] == nil {
			vendors[vendor] = Row{"id": "native:" + vendor, "name": route.Vendor + " 专用协议", "baseURL": route.Base, "types": []any{}, "modelsPath": ""}
		}
		entry := vendors[vendor]
		if entry["baseURL"] == "" && route.Base != "" {
			entry["baseURL"] = route.Base
		}
		found := false
		for _, kind := range array(entry["types"]) {
			if kind == route.Modality {
				found = true
			}
		}
		if !found {
			entry["types"] = append(array(entry["types"]), route.Modality)
		}
		// These adapters accept an OpenAI-compatible model directory. Servers
		// may disable it; administrators can change the path or add models manually.
		switch vendor {
		case "apiyi", "ccgo", "closeai", "dlapi", "infotoken", "ld", "mikotopro", "myapi", "mynewapi", "official-openai", "openrouter", "rolldek", "wxart", "zzshu":
			entry["modelsPath"] = "/v1/models"
			// The shared compatible text adapter is not restricted to text models
			// that happened to appear in the old, finite CSV export.
			hasText := false
			for _, kind := range array(entry["types"]) {
				if kind == "text" {
					hasText = true
				}
			}
			if !hasText {
				entry["types"] = append(array(entry["types"]), "text")
			}
		}
	}
	entries := []Row{}
	for _, entry := range vendors {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return str(entries[i]["id"]) < str(entries[j]["id"]) })
	for _, p := range []struct{ id, name string }{
		{"opencrow", "OpenCrow 专用协议"}, {"openai-image", "OpenAI 兼容图片"}, {"openai-video", "OpenAI 兼容视频"},
		{"json-async", "通用 JSON 异步任务"}, {"openvideo", "OpenVideo"}, {"suda", "Suda"}, {"modelhub", "ModelHub"},
		{"gway", "Gway"}, {"catcoder", "Catcoder"}, {"wxart", "WXArt"}, {"rolldek", "Rolldek"}, {"maitoken", "Mai-Token"},
		{"media-upload", "公共素材上传"}, {"demo", "本地演示"},
	} {
		base, path := "", "/models"
		types := []any{"image", "video"}
		if p.id == "opencrow" {
			base, path, types = "https://api.router.ai/v1", "/v1/models", []any{"image", "video", "text", "audio"}
		}
		if p.id == "demo" || p.id == "media-upload" {
			path = ""
		}
		if p.id == "demo" {
			types = []any{"image", "video", "text", "audio", "upscale"}
		}
		if p.id == "media-upload" {
			types = []any{}
		}
		entries = append(entries, Row{"id": p.id, "name": p.name, "baseURL": base, "modelsPath": path, "types": types})
	}
	return entries
}

// Built-in protocol metadata is immutable. Provider account settings and keys
// still come from the database on each request; they are never cached here.
var protocolRegistry = buildProviderProtocols()
var protocolByID = func() map[string]Row {
	entries := map[string]Row{}
	for _, p := range protocolRegistry {
		entries[str(p["id"])] = p
	}
	return entries
}()

func providerProtocols() []Row { return protocolRegistry }

func providerProtocol(protocol string) Row {
	return protocolByID[protocol]
}

func providerModelsPath(provider Row) string {
	if value, ok := object(provider["config"])["modelsPath"]; ok {
		return str(value)
	}
	return str(providerProtocol(str(provider["protocol"]))["modelsPath"])
}

func nativeModelRoute(provider Row, upstream, kind string, entryConfig Row) InventoryRoute {
	vendor := nativeVendor(provider["protocol"])
	r := InventoryRoute{Vendor: vendor, Model: upstream, Upstream: upstream, Modality: kind, Config: Row{}}
	for _, known := range providerPresetRoutes {
		if strings.EqualFold(known.Vendor, vendor) && known.Modality == kind && known.Upstream == upstream {
			r.Model, r.Config = known.Model, clone(known.Config)
			break
		}
	}
	for _, config := range []Row{object(provider["config"]), entryConfig} {
		for _, name := range []string{"sourceModel", "chatAPI", "submit_path_override", "poll_path_override"} {
			if str(config[name]) != "" {
				r.Config[name] = config[name]
			}
		}
	}
	if str(r.Config["sourceModel"]) != "" {
		r.Model = str(r.Config["sourceModel"])
	}
	r.Config["vendor"], r.Config["sourceModel"], r.Config["upstreamModel"] = vendor, r.Model, upstream
	return r
}

func validateNativeModelConfig(config Row) Row {
	result := Row{}
	for _, name := range []string{"sourceModel", "chatAPI", "submit_path_override", "poll_path_override"} {
		if config[name] == nil {
			continue
		}
		value := trimField(config, name, 0, 500)
		if strings.HasSuffix(name, "_path_override") && value != "" && !inventoryPathValid(value) {
			fail(400, "协议接口路径不正确")
		}
		if name == "chatAPI" && value != "" && value != "responses" && value != "chat-completions" {
			fail(400, "文本接口请选择 responses 或 chat-completions")
		}
		result[name] = value
	}
	return result
}

func providerSupportsKind(provider Row, kind string) bool {
	if provider["protocol"] == "inventory" {
		return generationKind(kind)
	}
	for _, supported := range array(providerProtocol(str(provider["protocol"]))["types"]) {
		if supported == kind {
			return true
		}
	}
	return false
}

func validateProviderModelKind(provider Row, kind string) {
	if providerSupportsKind(provider, kind) {
		return
	}
	fail(400, "此接入协议未适配所选生成类型")
}

func validateProviderKey(protocol any, key string) {
	if key == "" {
		return
	}
	if strings.IndexFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		fail(400, "API Key 不能包含空格或控制字符")
	}
	if (nativeVendor(protocol) != "" || protocol == "opencrow") && !inventoryCredentialReady(key) {
		fail(400, "请输入有效的供应商 API Key")
	}
}

func (a *App) nativeProvider(s Store, model, provider Row) Row {
	entry := s.One("SELECT * FROM provider_models WHERE provider_id=? AND upstream_model=?", provider["id"], model["upstream_model"])
	r := nativeModelRoute(provider, str(model["upstream_model"]), str(model["type"]), object(entry["config"]))
	result := clone(provider)
	config := clone(object(provider["config"]))
	for name, value := range r.Config {
		config[name] = value
	}
	result["protocol"], result["config"] = "inventory", config
	if str(entry["api_key"]) != "" {
		result["api_key"] = entry["api_key"]
	}
	key, err := a.decrypt(str(result["api_key"]))
	if err != nil || !inventoryCredentialReady(key) {
		fail(400, "请先配置供应商密钥")
	}
	return result
}

func (a *App) protocolRoutes(m *http.ServeMux) {
	a.route(m, "GET /api/admin/provider-protocols", 3, func(r *Request) any { return providerProtocols() })
	a.route(m, "POST /api/admin/providers/{id}/key", 3, func(r *Request) any {
		p := a.Store.One("SELECT api_key FROM providers WHERE id=?", r.ID())
		if p == nil {
			fail(404, "供应商不存在")
		}
		key, err := a.decrypt(str(p["api_key"]))
		if err != nil {
			fail(409, "密钥无法解密，请重新配置")
		}
		a.Store.Audit(r.UID(), "provider.key.view", Row{"id": r.ID()})
		return Row{"apiKey": key}
	})
}

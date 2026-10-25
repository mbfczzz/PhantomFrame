package api

import (
	"net/http/httptest"
	"testing"
)

func TestSpecificationPricingAndBillingUnits(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin, user := &testClient{server: server.URL}, &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	user.ok(t, "POST", "/auth/register", Row{"username": "pricing-owner", "password": "Pricing-pass-2026"}, 200)
	params := []Row{
		{"name": "resolution", "label": "分辨率", "type": "select", "default": "1k", "options": []string{"1k", "2k", "4k"}},
		{"name": "quality", "label": "质量", "type": "select", "default": "standard", "options": []string{"standard", "high"}},
		{"name": "n", "label": "生成张数", "type": "number", "default": 1, "min": 1, "max": 4},
	}
	model := object(admin.ok(t, "POST", "/admin/models", Row{"id": "tier-image", "name": "Tier image", "type": "image", "provider_id": "demo", "upstream_model": "tier-image", "parameters": params}, 201))
	path := "/admin/models/tier-image/prices"
	rules := []Row{}
	for _, tier := range []struct {
		resolution, quality string
		price               float64
	}{{"1k", "standard", .1}, {"2k", "standard", .25}, {"4k", "standard", .5}, {"1k", "high", .2}, {"2k", "high", .4}} {
		rules = append(rules, Row{"label": tier.resolution + "/" + tier.quality, "unit": "request", "credits": tier.price, "conditions": Row{"resolution": tier.resolution, "quality": tier.quality}})
	}
	payload := Row{"expectedVersion": model["pricingVersion"], "prices": rules}
	user.ok(t, "PUT", path, payload, 403)
	key := object(admin.ok(t, "POST", "/keys", Row{"name": "billing-admin-key"}, 201))
	keyClient := &testClient{server: server.URL, key: str(key["token"])}
	keyClient.ok(t, "PUT", path, payload, 403)
	model = object(admin.ok(t, "PUT", path, payload, 200))
	want(t, len(array(model["prices"])), 5)
	for _, scenario := range []struct {
		resolution, quality string
		count               int
		total               float64
	}{{"1k", "standard", 1, .1}, {"2k", "standard", 1, .25}, {"4k", "standard", 1, .5}, {"1k", "high", 1, .2}, {"2k", "high", 1, .4}} {
		request := Row{"modelId": "tier-image", "params": Row{"resolution": scenario.resolution, "quality": scenario.quality, "n": scenario.count}}
		q := object(user.ok(t, "POST", "/pricing/quote", request, 200))
		want(t, q["unit"], "request")
		want(t, q["quantity"], scenario.count)
		want(t, q["total"], scenario.total)
		request["prompt"] = "Local billing check"
		request["expectedCost"] = scenario.total
		job := object(user.ok(t, "POST", "/jobs", request, 202))
		want(t, job["cost"], scenario.total)
		want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 100-scenario.total)
		user.ok(t, "POST", "/jobs/"+str(job["id"])+"/cancel", nil, 200)
		want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 100)
	}
	user.ok(t, "POST", "/pricing/quote", Row{"modelId": "tier-image", "params": Row{"resolution": "4k", "quality": "high"}}, 400)
	user.ok(t, "POST", "/jobs", Row{"modelId": "tier-image", "prompt": "Unpriced tier", "params": Row{"resolution": "4k", "quality": "high"}}, 400)
	want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 100)
	// Stale editors and an invalid later row cannot partially change live prices.
	admin.ok(t, "PUT", path, payload, 409)
	before := jsonText(a.Store.ModelView(a.Store.One("SELECT * FROM models WHERE id='tier-image'")))
	firstRule := clone(object(array(model["prices"])[0]))
	firstRule["credits"] = 9.0
	invalid := Row{"label": "unsupported", "unit": "request", "credits": 1, "conditions": Row{"resolution": "8k"}}
	admin.ok(t, "PUT", path, Row{"expectedVersion": model["pricingVersion"], "prices": []Row{firstRule, invalid}}, 400)
	want(t, jsonText(a.Store.ModelView(a.Store.One("SELECT * FROM models WHERE id='tier-image'"))), before)
	admin.ok(t, "POST", "/admin/prices", Row{"model_id": "tier-image", "label": "invalid unit", "unit": "second", "credits": 1}, 400)
	admin.ok(t, "POST", "/admin/prices", Row{"model_id": "tier-image", "label": "old image unit", "unit": "image", "credits": 1}, 400)
	// Video unit prices and flat request prices have deliberately different quantities.
	for _, unit := range []string{"second", "request"} {
		mid := "video-" + unit
		admin.ok(t, "POST", "/admin/models", Row{"id": mid, "name": mid, "type": "video", "provider_id": "demo", "upstream_model": mid, "parameters": []Row{{"name": "duration", "label": "时长", "type": "number", "default": 5, "min": 1, "max": 15}}}, 201)
		admin.ok(t, "POST", "/admin/prices", Row{"model_id": mid, "label": unit, "unit": unit, "credits": .1}, 200)
		for _, duration := range []int{5, 10} {
			q := object(user.ok(t, "POST", "/pricing/quote", Row{"modelId": mid, "params": Row{"duration": duration}}, 200))
			quantity := 1
			if unit == "second" {
				quantity = duration
			}
			want(t, q["quantity"], quantity)
			want(t, q["total"], float64(quantity)/10)
		}
		admin.ok(t, "POST", "/admin/prices", Row{"model_id": mid, "label": "invalid image unit", "unit": "image", "credits": 1}, 400)
	}
	admin.ok(t, "POST", "/admin/models", Row{"id": "image-request", "name": "Image request", "type": "image", "provider_id": "demo", "upstream_model": "image", "parameters": params}, 201)
	admin.ok(t, "POST", "/admin/prices", Row{"model_id": "image-request", "label": "per request", "unit": "request", "credits": .3}, 200)
	user.ok(t, "POST", "/pricing/quote", Row{"modelId": "image-request", "params": Row{"n": 4}}, 400)
	q := object(user.ok(t, "POST", "/pricing/quote", Row{"modelId": "image-request", "params": Row{"n": 1}}, 200))
	want(t, q["quantity"], 1)
	want(t, q["total"], .3)
	firstRule["model_id"] = "image-request"
	admin.ok(t, "PUT", "/admin/prices/"+str(firstRule["id"]), firstRule, 400)
	want(t, jsonText(a.Store.ModelView(a.Store.One("SELECT * FROM models WHERE id='tier-image'"))), before)
}

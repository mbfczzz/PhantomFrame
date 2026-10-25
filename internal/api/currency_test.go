package api

import (
	"net/http/httptest"
	"testing"
)

func TestCNYAmountsAndPrecision(t *testing.T) {
	a := setupIntegration(t)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	user := &testClient{server: server.URL}
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	owner := object(user.ok(t, "POST", "/auth/register", Row{"username": "cny-owner", "password": "Currency-pass-2026"}, 200))
	uid := str(owner["id"])
	want(t, owner["currency"], "CNY")
	want(t, owner["balance"], 100)
	want(t, a.Store.One("SELECT balance FROM users WHERE id=?", uid)["balance"], 10000)
	config := object(user.ok(t, "GET", "/config", nil, 200))
	want(t, config["currency"], "CNY")
	want(t, config["creditValueInYuan"], 1)
	model := Row{"id": "cny-model", "name": "CNY model", "type": "video", "provider_id": "demo", "upstream_model": "cny", "parameters": []Row{{"name": "duration", "label": "时长", "type": "number", "default": 3}}}
	admin.ok(t, "POST", "/admin/models", model, 201)
	priceBody := Row{"model_id": "cny-model", "label": "per second", "unit": "second", "credits": 0.1}
	price := object(admin.ok(t, "POST", "/admin/prices", priceBody, 200))
	want(t, price["currency"], "CNY")
	want(t, price["credits"], 0.1)
	want(t, a.Store.One("SELECT credits FROM prices WHERE id=?", price["id"])["credits"], 10)
	quote := object(user.ok(t, "POST", "/pricing/quote", Row{"modelId": "cny-model"}, 200))
	want(t, quote["total"], 0.3)
	want(t, quote["unitPrice"], 0.1)
	want(t, quote["currency"], "CNY")
	job := object(user.ok(t, "POST", "/jobs", Row{"modelId": "cny-model", "prompt": "RMB decimal test", "expectedCost": 0.3}, 202))
	want(t, job["cost"], 0.3)
	want(t, job["currency"], "CNY")
	want(t, a.Store.One("SELECT balance FROM users WHERE id=?", uid)["balance"], 9970)
	user.ok(t, "POST", "/jobs/"+str(job["id"])+"/cancel", nil, 200)
	want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 100)
	outgoing := object(user.ok(t, "GET", "/transactions?kind=generation", nil, 200))
	want(t, object(outgoing["summary"])["expense"], 0.3)
	want(t, object(outgoing["summary"])["unit"], "CNY")
	want(t, object(array(outgoing["items"])[0])["amount"], -0.3)
	refund := object(user.ok(t, "GET", "/transactions?kind=refund", nil, 200))
	want(t, object(array(refund["items"])[0])["amount"], 0.3)
	codes := array(admin.ok(t, "POST", "/admin/codes", Row{"count": 1, "credits": 0.25}, 201))
	receipt := object(user.ok(t, "POST", "/redeem", Row{"code": codes[0]}, 200))
	want(t, receipt["credits"], 0.25)
	want(t, receipt["balance"], 100.25)
	want(t, receipt["currency"], "CNY")
	user.ok(t, "POST", "/redeem", Row{"code": codes[0]}, 400)
	child := object(user.ok(t, "POST", "/sub-users", Row{"username": "cny-child", "password": "Currency-pass-2026"}, 201))
	user.ok(t, "POST", "/sub-users/"+str(child["id"])+"/transfer", Row{"credits": 0.1}, 200)
	want(t, a.Store.One("SELECT balance FROM users WHERE id=?", child["id"])["balance"], 10)
	admin.ok(t, "POST", "/admin/users/"+uid+"/credits", Row{"amount": -0.05, "reason": "Decimal adjustment"}, 200)
	want(t, object(user.ok(t, "GET", "/me", nil, 200))["balance"], 100.1)
	priceBody["credits"] = 0.0000001
	admin.ok(t, "PUT", "/admin/prices/"+str(price["id"]), priceBody, 400)
	want(t, a.Store.One("SELECT credits FROM prices WHERE id=?", price["id"])["credits"], 10)
	admin.ok(t, "POST", "/admin/codes", Row{"count": 1, "credits": 0.011}, 400)
	admin.ok(t, "POST", "/admin/users/"+uid+"/credits", Row{"amount": 0.001, "reason": "Invalid precision"}, 400)
	user.ok(t, "POST", "/sub-users/"+str(child["id"])+"/transfer", Row{"credits": 0.101}, 400)
	user.ok(t, "POST", "/jobs", Row{"modelId": "cny-model", "prompt": "Invalid expected precision", "expectedCost": 0.301}, 400)
	settings := object(admin.ok(t, "GET", "/admin/settings", nil, 200))
	for _, amount := range []string{"0.001", "not-a-number"} {
		settings["welcomeCredits"] = amount
		admin.ok(t, "PUT", "/admin/settings", settings, 400)
	}
	want(t, a.Store.One("SELECT balance FROM users WHERE id=?", uid)["balance"], 10010)
	want(t, object(user.ok(t, "GET", "/usage", nil, 200))["currency"], "CNY")
	if err := a.Migrate(); err != nil {
		t.Fatal(err)
	}
	a.Seed()
	want(t, a.Store.One("SELECT balance FROM users WHERE id=?", uid)["balance"], 10010)
	want(t, a.Store.One("SELECT credits FROM prices WHERE id=?", price["id"])["credits"], 10)
}

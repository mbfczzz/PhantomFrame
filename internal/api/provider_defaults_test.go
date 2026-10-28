package api

import (
	"net/http/httptest"
	"sync"
	"testing"
)

func TestOpenCrowStartupDefaultsPreserveConfiguration(t *testing.T) {
	a := setupIntegration(t)
	initial := a.Store.One("SELECT * FROM providers WHERE protocol='opencrow'")
	if initial == nil {
		t.Fatal("OpenCrow was not prepared during startup")
	}
	want(t, initial["name"], "OpenCrow")
	want(t, initial["base_url"], "https://api.router.ai/v1")
	want(t, initial["api_key"], "")
	want(t, truth(initial["enabled"]), true)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a.ensureOpenCrowDefault() }()
	}
	wg.Wait()
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE protocol='opencrow'")["n"], 1)
	want(t, a.Store.One("SELECT count(*) n FROM models WHERE provider_id=?", initial["id"])["n"], 0)
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	client := &testClient{server: server.URL}
	client.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	provider := object(client.ok(t, "PUT", "/admin/providers/"+str(initial["id"]), Row{"name": "我的 OpenCrow", "protocol": "opencrow", "base_url": "https://custom-gateway.invalid/v1", "apiKey": "local-default-preservation-key", "enabled": false, "config": Row{"uploadProviderId": "custom-upload"}}, 200))
	want(t, truth(provider["hasKey"]), true)
	before := jsonText(a.Store.One("SELECT * FROM providers WHERE id=?", initial["id"]))
	balance := jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id"))
	a.Seed()
	a.Seed()
	want(t, jsonText(a.Store.One("SELECT * FROM providers WHERE id=?", initial["id"])), before)
	want(t, jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id")), balance)
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE protocol='opencrow'")["n"], 1)
}

func TestEmptyOpenCrowPresetDoesNotBlockDemoInitialization(t *testing.T) {
	a := setupIntegration(t)
	a.Tx(func(s Store) {
		s.Exec("DELETE FROM prices")
		s.Exec("DELETE FROM models")
		s.Exec("DELETE FROM provider_models")
		s.Exec("DELETE FROM providers WHERE protocol='demo'")
	})
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE protocol='opencrow'")["n"], 1)
	a.Seed()
	want(t, a.Store.One("SELECT count(*) n FROM models WHERE provider_id='demo'")["n"], 22)
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE protocol='opencrow'")["n"], 1)
}

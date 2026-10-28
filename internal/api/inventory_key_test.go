package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInventoryKeyRevealRequiresAdminSessionAndDoesNotMutate(t *testing.T) {
	a := setupIntegration(t)
	a.ensureProviderPresets()
	rid := providerPresetRoutes[0].ID
	path := "/admin/provider-inventory/" + rid + "/key"
	secret := "inventory-reveal-test-only-secret"
	a.Store.Exec("UPDATE inventory_routes SET api_key=? WHERE id=?", a.encrypt(secret), rid)
	before := jsonText(a.Store.One("SELECT * FROM inventory_routes WHERE id=?", rid))
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	admin := &testClient{server: server.URL}
	admin.ok(t, "POST", path, Row{}, 401)
	user := &testClient{server: server.URL}
	user.ok(t, "POST", "/auth/register", Row{"username": "key-view-user", "password": "Test-user-password-2026"}, 200)
	user.ok(t, "POST", path, Row{}, 403)
	admin.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	token := object(admin.ok(t, "POST", "/keys", Row{"name": "reveal access fixture"}, 201))
	apiClient := &testClient{server: server.URL, key: str(token["token"])}
	apiClient.ok(t, "POST", path, Row{}, 403)
	status, _ := admin.request(t, "POST", path, Row{}, map[string]string{"Origin": "https://foreign.invalid"})
	want(t, status, 403)
	want(t, a.Store.One("SELECT count(*) n FROM audit_log WHERE action='inventory.key.view'")["n"], 0)

	req, err := http.NewRequest("POST", server.URL+"/api"+path, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", admin.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	want(t, res.StatusCode, 200)
	want(t, res.Header.Get("Cache-Control"), "no-store")
	var result Row
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	want(t, result["apiKey"], secret)
	want(t, jsonText(a.Store.One("SELECT * FROM inventory_routes WHERE id=?", rid)), before)
	want(t, a.Store.One("SELECT count(*) n FROM audit_log WHERE action='inventory.key.view'")["n"], 1)
	for _, listing := range []string{"/admin/provider-inventory", "/admin/providers", "/admin/models", "/admin/audit"} {
		if strings.Contains(jsonText(admin.ok(t, "GET", listing, nil, 200)), secret) {
			t.Fatalf("credential leaked through %s", listing)
		}
	}
	admin.ok(t, "POST", "/admin/provider-inventory/missing-route/key", Row{}, 404)
	a.Store.Exec("UPDATE inventory_routes SET api_key='' WHERE id=?", rid)
	want(t, object(admin.ok(t, "POST", path, Row{}, 200))["apiKey"], "")
	a.Store.Exec("UPDATE inventory_routes SET api_key='invalid-encrypted-fixture' WHERE id=?", rid)
	response := admin.ok(t, "POST", path, Row{}, 409)
	if strings.Contains(jsonText(response), "invalid-encrypted-fixture") {
		t.Fatal("decrypt error leaked ciphertext")
	}
}

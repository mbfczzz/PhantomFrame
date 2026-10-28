package api

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestProviderPresetsContainAllNonsecretRoutes(t *testing.T) {
	want(t, len(providerPresetRoutes), 249)
	vendors := map[string]bool{}
	missing := 0
	for _, r := range providerPresetRoutes {
		vendors[strings.ToLower(r.Vendor)] = true
		want(t, r.Key, "")
		if r.Modality == "3d" {
			t.Fatal("3D preset included")
		}
		if r.Base == "" {
			missing++
		}
		if r.Upstream == "" {
			t.Fatal("preset lost model mapping")
		}
	}
	want(t, len(vendors), 34)
	want(t, missing, 6)
	if strings.Contains(string(providerPresetJSON), "api_key") || strings.Contains(string(providerPresetJSON), "test-only-key-") {
		t.Fatal("credential fields must not be embedded in production presets")
	}
	parsed, _, err := ReadProviderInventory(strings.NewReader(inventoryFixtureCSV(t)))
	if err != nil {
		t.Fatal(err)
	}
	want(t, len(parsed), len(providerPresetRoutes))
	for i, r := range parsed {
		r.Key = ""
		want(t, jsonText(providerPresetRoutes[i]), jsonText(r))
	}
}

func TestAllProviderPresetsStartOnceAndPreserveReviewedData(t *testing.T) {
	a := setupIntegration(t)
	a.Tx(func(s Store) {
		s.Exec("DELETE FROM prices")
		s.Exec("DELETE FROM models")
		s.Exec("DELETE FROM provider_models")
		s.Exec("DELETE FROM providers")
	})
	a.Config.Demo = false
	a.Config.ProviderPresets = true
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- try(a.ensureProviderPresets) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	a.Seed()
	want(t, a.Store.One("SELECT count(*) n FROM providers")["n"], 35)
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE protocol='inventory'")["n"], 34)
	want(t, a.Store.One("SELECT count(*) n FROM inventory_routes")["n"], 249)
	want(t, a.Store.One("SELECT count(*) n FROM inventory_routes WHERE api_key<>''")["n"], 0)
	want(t, a.Store.One("SELECT count(*) n FROM models WHERE enabled=1")["n"], 0)
	want(t, a.Store.One("SELECT count(*) n FROM models")["n"], 249)
	want(t, a.Store.One("SELECT count(*) n FROM prices")["n"], 0)
	want(t, a.Store.One("SELECT count(*) n FROM jobs")["n"], 0)
	routes := a.Store.All("SELECT * FROM inventory_routes WHERE vendor='APIYI' ORDER BY id LIMIT 2")
	for i, r := range routes {
		a.Store.Exec("UPDATE inventory_routes SET api_key=?,base_url=?,source_enabled=0 WHERE id=?", a.encrypt("independent-route-fixture-"+str(i)), "https://reviewed.invalid/"+str(i), r["id"])
	}
	a.Store.Exec("UPDATE providers SET name='reviewed-supplier',enabled=0 WHERE id=?", routes[0]["provider_id"])
	mid := inventoryModelID(str(routes[0]["id"]))
	a.Store.Exec("UPDATE models SET name='reviewed-model',parameters='[]' WHERE id=?", mid)
	a.Store.Exec("INSERT INTO prices(id,model_id,label,unit,credits,unit_price_micros,conditions) VALUES(?,?,'reviewed','request',12,123456,'{}')", id(), mid)
	beforeRoutes := jsonText(a.Store.All("SELECT * FROM inventory_routes ORDER BY id"))
	beforeModels := jsonText(a.Store.All("SELECT * FROM models ORDER BY id"))
	beforePrices := jsonText(a.Store.All("SELECT * FROM prices ORDER BY id"))
	beforeBalance := jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id"))
	a.Seed()
	a.Seed()
	want(t, jsonText(a.Store.All("SELECT * FROM inventory_routes ORDER BY id")), beforeRoutes)
	want(t, jsonText(a.Store.All("SELECT * FROM models ORDER BY id")), beforeModels)
	want(t, jsonText(a.Store.All("SELECT * FROM prices ORDER BY id")), beforePrices)
	want(t, jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id")), beforeBalance)
	want(t, a.Store.One("SELECT name FROM providers WHERE id=?", routes[0]["provider_id"])["name"], "reviewed-supplier")
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	list := array(c.ok(t, "GET", "/admin/provider-inventory", nil, 200))
	want(t, len(list), 249)
	for _, r := range list {
		if object(r)["providerId"] == nil {
			t.Fatal("route missing supplier filter identity")
		}
	}
	if strings.Contains(jsonText(list), "independent-route-fixture-") {
		t.Fatal("route list leaked key")
	}
}

func TestProviderPresetInitializationPreservesImportedCredentials(t *testing.T) {
	a := setupIntegration(t)
	if _, err := a.ImportProviderInventory(strings.NewReader(inventoryFixtureCSV(t)), true); err != nil {
		t.Fatal(err)
	}
	before := jsonText(a.Store.All("SELECT * FROM inventory_routes ORDER BY id"))
	a.Config.ProviderPresets = true
	a.Seed()
	want(t, jsonText(a.Store.All("SELECT * FROM inventory_routes ORDER BY id")), before)
}

func TestProviderPresetsInheritCompatibleExistingPrice(t *testing.T) {
	a := setupIntegration(t)
	pid, mid := id(), id()
	a.Store.Exec("INSERT INTO providers(id,name,protocol,base_url,api_key,enabled,config) VALUES(?,'reviewed source','openai-image','http://127.0.0.1:9','',0,'{}')", pid)
	a.Store.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,category,enabled,tags,parameters) VALUES(?,'gpt-image-2','image',?,'gpt-image-2','','test',0,'[]','[]')", mid, pid)
	a.Store.Exec("INSERT INTO prices(id,model_id,label,unit,credits,unit_price_micros,conditions) VALUES(?,?,'existing retail','request',12,123456,'{}')", id(), mid)
	a.Config.ProviderPresets = true
	a.Seed()
	rows := a.Store.All("SELECT pr.* FROM prices pr JOIN models m ON m.id=pr.model_id JOIN inventory_routes r ON r.id=m.inventory_route_id WHERE r.model='gpt-image-2' AND r.modality='image'")
	if len(rows) == 0 {
		t.Fatal("compatible same-name prices were not inherited")
	}
	for _, p := range rows {
		want(t, priceMicros(p), int64(123456))
	}
	want(t, a.Store.One("SELECT count(*) n FROM models WHERE inventory_route_id IS NOT NULL AND enabled=1")["n"], 0)
}

func TestProviderPresetsRepairMissingModelFromReviewedRoute(t *testing.T) {
	a := setupIntegration(t)
	a.ensureProviderPresets()
	r := providerPresetRoutes[0]
	mid := inventoryModelID(r.ID)
	a.Store.Exec("UPDATE inventory_routes SET base_url='https://reviewed.invalid',upstream_model='reviewed-upstream',api_key=?,source_enabled=0 WHERE id=?", a.encrypt("reviewed-route-fixture-key"), r.ID)
	a.Store.Exec("DELETE FROM prices WHERE model_id=?", mid)
	a.Store.Exec("DELETE FROM models WHERE id=?", mid)
	before := jsonText(a.Store.All("SELECT * FROM inventory_routes ORDER BY id"))
	a.ensureProviderPresets()
	m := a.Store.One("SELECT * FROM models WHERE id=?", mid)
	if m == nil {
		t.Fatal("startup left an existing route without its model")
	}
	want(t, m["upstream_model"], "reviewed-upstream")
	want(t, m["enabled"], 0)
	want(t, m["inventory_route_id"], r.ID)
	want(t, jsonText(a.Store.All("SELECT * FROM inventory_routes ORDER BY id")), before)
	want(t, a.Store.One("SELECT count(*) n FROM models WHERE inventory_route_id IS NOT NULL")["n"], 249)
}

func TestInventoryImportRejectsOccupiedIdentities(t *testing.T) {
	for _, conflict := range []string{"model", "provider"} {
		t.Run(conflict, func(t *testing.T) {
			a := setupIntegration(t)
			r := providerPresetRoutes[0]
			if conflict == "model" {
				a.Store.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,parameters,tags) VALUES(?,'existing custom','image','demo','custom-upstream','','[]','[]')", inventoryModelID(r.ID))
			} else {
				a.Store.Exec("INSERT INTO providers(id,name,protocol,api_key,config) VALUES(?,'existing custom','openai-image','','{}')", inventoryProviderID(r.Vendor))
			}
			beforeModels := jsonText(a.Store.All("SELECT * FROM models ORDER BY id"))
			beforeProviders := jsonText(a.Store.All("SELECT * FROM providers ORDER BY id"))
			if _, err := a.ImportProviderInventory(strings.NewReader(inventoryFixtureCSV(t)), true); err == nil {
				t.Fatal("import accepted an identity belonging to another configuration")
			}
			want(t, jsonText(a.Store.All("SELECT * FROM models ORDER BY id")), beforeModels)
			want(t, jsonText(a.Store.All("SELECT * FROM providers ORDER BY id")), beforeProviders)
			want(t, a.Store.One("SELECT count(*) n FROM inventory_routes")["n"], 0)
		})
	}
}

func TestInventoryRouteAddressChangeRequiresReview(t *testing.T) {
	a := setupIntegration(t)
	a.ensureProviderPresets()
	r := providerPresetRoutes[0]
	mid := inventoryModelID(r.ID)
	a.Store.Exec("UPDATE inventory_routes SET base_url='https://reviewed.invalid/v1',api_key=?,source_enabled=1 WHERE id=?", a.encrypt("reviewed-route-fixture-key"), r.ID)
	a.Store.Exec("INSERT INTO prices(id,model_id,label,unit,credits,conditions) VALUES(?,?,'reviewed','request',10,'{}')", id(), mid)
	prices := jsonText(a.Store.All("SELECT * FROM prices WHERE model_id=?", mid))
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	for _, tc := range []struct {
		name    string
		body    Row
		enabled int
	}{
		{"same normalized address", Row{"baseURL": "https://reviewed.invalid/v1/", "apiKey": ""}, 1},
		{"rotate key", Row{"apiKey": "rotated-route-fixture-key"}, 1},
		{"change address", Row{"baseURL": "https://different.invalid/v1"}, 0},
		{"clear address", Row{"baseURL": ""}, 0},
		{"disable route", Row{"sourceEnabled": false}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.Store.Exec("UPDATE models SET enabled=1 WHERE id=?", mid)
			c.ok(t, "PATCH", "/admin/provider-inventory/"+r.ID, tc.body, 200)
			want(t, a.Store.One("SELECT enabled FROM models WHERE id=?", mid)["enabled"], tc.enabled)
			want(t, jsonText(a.Store.All("SELECT * FROM prices WHERE model_id=?", mid)), prices)
		})
	}
}

func TestProviderPresetConcurrentImport(t *testing.T) {
	a := setupIntegration(t)
	input := inventoryFixtureCSV(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() { <-start; results <- try(a.ensureProviderPresets) }()
	go func() { <-start; _, err := a.ImportProviderInventory(strings.NewReader(input), true); results <- err }()
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	want(t, a.Store.One("SELECT count(*) n FROM inventory_routes")["n"], 249)
	want(t, a.Store.One("SELECT count(*) n FROM models WHERE inventory_route_id IS NOT NULL")["n"], 249)
	parsed, _, err := ReadProviderInventory(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range parsed {
		key, err := a.decrypt(str(a.Store.One("SELECT api_key FROM inventory_routes WHERE id=?", r.ID)["api_key"]))
		if err != nil {
			t.Fatal(err)
		}
		if key != r.Key {
			t.Fatal("concurrent startup changed imported credential")
		}
	}
}

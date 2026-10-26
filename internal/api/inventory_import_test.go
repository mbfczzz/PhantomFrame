package api

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// This fixture contains only deterministic fake keys, never exported credentials.
//
//go:embed inventory_fixture_test.json
var inventoryTestFixture []byte

func inventoryFixtureCSV(t *testing.T) string {
	t.Helper()
	var rows []map[string]string
	if e := json.Unmarshal(inventoryTestFixture, &rows); e != nil {
		t.Fatal(e)
	}
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	keys := []string{"route_id", "provider", "model", "modality", "base_url", "api_key", "enabled", "is_primary", "priority", "weight", "upstream_model", "model_map", "chat_api", "operation", "submit_path_override", "poll_path_override"}
	_ = writer.Write(keys)
	for _, r := range rows {
		values := []string{}
		for _, key := range keys {
			values = append(values, r[key])
		}
		_ = writer.Write(values)
	}
	writer.Flush()
	return output.String()
}

func TestInventoryCompleteImportAndIsolation(t *testing.T) {
	a := setupIntegration(t)
	input := inventoryFixtureCSV(t)
	report, e := a.ImportProviderInventory(strings.NewReader(input), false)
	if e != nil {
		t.Fatal(e)
	}
	want(t, report["routes"], 249)
	want(t, report["providers"], 34)
	want(t, report["excluded3D"], 1)
	want(t, len(report["warnings"].([]Row)), 92)
	want(t, a.Store.One("SELECT count(*) n FROM inventory_routes")["n"], 0)
	balance := jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id"))
	for i := 0; i < 2; i++ {
		if _, e := a.ImportProviderInventory(strings.NewReader(input), true); e != nil {
			t.Fatal(e)
		}
	}
	want(t, a.Store.One("SELECT count(*) n FROM inventory_routes")["n"], 249)
	want(t, a.Store.One("SELECT count(*) n FROM providers WHERE protocol='inventory'")["n"], 34)
	want(t, a.Store.One("SELECT count(*) n FROM models WHERE inventory_route_id IS NOT NULL")["n"], 249)
	want(t, a.Store.One("SELECT count(*) n FROM models WHERE inventory_route_id IS NOT NULL AND enabled=1")["n"], 0)
	want(t, a.Store.One("SELECT count(*) n FROM inventory_routes WHERE modality='3d'")["n"], 0)
	want(t, jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id")), balance)
	for _, r := range a.Store.All("SELECT api_key FROM inventory_routes WHERE api_key<>''") {
		if strings.Contains(str(r["api_key"]), "test-only-key") {
			t.Fatal("plaintext key stored")
		}
		key, e := a.decrypt(str(r["api_key"]))
		if e != nil || !strings.HasPrefix(key, "test-only-key-") {
			t.Fatal("route credential was lost")
		}
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "GET", "/admin/provider-inventory", nil, 401)
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	listing := c.ok(t, "GET", "/admin/provider-inventory", nil, 200)
	want(t, len(array(listing)), 249)
	if strings.Contains(jsonText(listing), "test-only-key-") {
		t.Fatal("route list exposed a key")
	}
	for _, path := range []string{"/admin/models", "/admin/providers", "/admin/audit"} {
		if strings.Contains(jsonText(c.ok(t, "GET", path, nil, 200)), "test-only-key-") {
			t.Fatalf("%s leaked a key", path)
		}
	}
}

func TestInventoryPreservesReviewedConfiguration(t *testing.T) {
	a := setupIntegration(t)
	input := inventoryFixtureCSV(t)
	if _, e := a.ImportProviderInventory(strings.NewReader(input), true); e != nil {
		t.Fatal(e)
	}
	row := a.Store.One("SELECT * FROM inventory_routes WHERE source_enabled=1 AND api_key<>'' AND base_url<>'' AND modality='image' LIMIT 1")
	mid := inventoryModelID(str(row["id"]))
	a.Store.Exec("UPDATE models SET enabled=1,name='reviewed-name' WHERE id=?", mid)
	a.Store.Exec("INSERT INTO prices(id,model_id,label,unit,credits,unit_price_micros,conditions) VALUES(?,?,'reviewed','request',35,350000,'{}')", id(), mid)
	encrypted := row["api_key"]
	if _, e := a.ImportProviderInventory(strings.NewReader(input), true); e != nil {
		t.Fatal(e)
	}
	want(t, a.Store.One("SELECT name FROM models WHERE id=?", mid)["name"], "reviewed-name")
	want(t, truth(a.Store.One("SELECT enabled FROM models WHERE id=?", mid)["enabled"]), true)
	want(t, a.Store.One("SELECT count(*) n FROM prices WHERE model_id=?", mid)["n"], 1)
	want(t, a.Store.One("SELECT api_key FROM inventory_routes WHERE id=?", row["id"])["api_key"], encrypted)
	if e := a.Migrate(); e != nil {
		t.Fatal(e)
	}
	want(t, a.Store.One("SELECT count(*) n FROM inventory_routes")["n"], 249)
}

func TestInventoryUnchangedStartupPreservesRouteRepairs(t *testing.T) {
	a := setupIntegration(t)
	input := inventoryFixtureCSV(t)
	if _, err := a.importProviderInventory(strings.NewReader(input), true, true); err != nil {
		t.Fatal(err)
	}
	route := a.Store.One("SELECT id FROM inventory_routes ORDER BY id LIMIT 1")
	a.Store.Exec("UPDATE inventory_routes SET base_url='http://127.0.0.1:9',api_key=?,source_enabled=0 WHERE id=?", a.encrypt("manually-repaired-test-key"), route["id"])
	report, err := a.importProviderInventory(strings.NewReader(input), true, true)
	if err != nil {
		t.Fatal(err)
	}
	want(t, report["unchanged"], true)
	want(t, report["applied"], false)
	saved := a.Store.One("SELECT * FROM inventory_routes WHERE id=?", route["id"])
	want(t, saved["base_url"], "http://127.0.0.1:9")
	key, err := a.decrypt(str(saved["api_key"]))
	if err != nil {
		t.Fatal(err)
	}
	want(t, key, "manually-repaired-test-key")
	want(t, truth(saved["source_enabled"]), false)
	if _, ok := a.Store.Settings()["_inventoryStartupFingerprint"]; ok {
		t.Fatal("internal import fingerprint leaked through settings")
	}
	// Explicit administrator import remains authoritative, even with unchanged CSV.
	report, err = a.ImportProviderInventory(strings.NewReader(input), true)
	if err != nil {
		t.Fatal(err)
	}
	want(t, report["applied"], true)
	saved = a.Store.One("SELECT * FROM inventory_routes WHERE id=?", route["id"])
	key, err = a.decrypt(str(saved["api_key"]))
	if err != nil {
		t.Fatal(err)
	}
	if key == "manually-repaired-test-key" {
		t.Fatal("explicit import did not apply source credentials")
	}
}

func TestInventoryRejectsMalformedAndSecretErrors(t *testing.T) {
	input := inventoryFixtureCSV(t)
	for name, value := range map[string]string{"missing_columns": "api_key\nprivate-test-key\n", "duplicate_ids": input + strings.Split(input, "\n")[1] + "\n", "invalid_csv": input + "\"private-test-key"} {
		t.Run(name, func(t *testing.T) {
			_, _, e := ReadProviderInventory(strings.NewReader(value))
			if e == nil {
				t.Fatal("invalid inventory accepted")
			}
			if strings.Contains(e.Error(), "private-test-key") {
				t.Fatal("parser error exposed credential")
			}
		})
	}
	for _, path := range []string{"//evil.example/path", "/../billing", "/%2e%2e/billing", "/x\\y"} {
		if inventoryPathValid(path) {
			t.Fatalf("unsafe override %s", path)
		}
	}
	for _, test := range []struct{ base, path, want string }{{"https://api.invalid/v1", "/v1/images/generations", "https://api.invalid/v1/images/generations"}, {"https://api.invalid/api/v1", "/v1/images", "https://api.invalid/api/v1/images"}, {"https://api.invalid/api", "/v1/images", "https://api.invalid/api/v1/images"}} {
		got, e := inventoryEndpoint(test.base, test.path)
		if e != nil {
			t.Fatal(e)
		}
		want(t, got, test.want)
	}
}

func TestInventoryEveryImportedRouteHasARequestPlan(t *testing.T) {
	routes, _, e := ReadProviderInventory(strings.NewReader(inventoryFixtureCSV(t)))
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range routes {
		t.Run(r.ID, func(t *testing.T) {
			params := Row{}
			for _, raw := range inventoryParameters(r) {
				p := object(raw)
				if p["type"] == "assets" {
					params[str(p["name"])] = []any{}
				} else {
					params[str(p["name"])] = p["default"]
				}
			}
			refs := map[string][]string{"image": {}, "video": {}, "audio": {}}
			if r.Modality == "upscale" {
				refs["video"] = []string{"https://reference.invalid/video.mp4"}
			}
			p := Row{"type": r.Modality, "upstreamModel": r.Upstream, "config": clone(r.Config)}
			object(p["config"])["upstreamModel"] = r.Upstream
			call, _, _, e := inventoryPlan(Row{"id": id(), "prompt": "local contract fixture", "params": params}, p, refs, nil)
			if e != nil {
				t.Fatal(e)
			}
			if call.Body == nil && call.Form == nil {
				t.Fatal("no request body")
			}
			if call.Path == "" && !strings.EqualFold(r.Vendor, "runware") {
				t.Fatal("missing endpoint")
			}
			if strings.Contains(jsonText(call.Body), r.Key) && r.Key != "" {
				t.Fatal("credential leaked into generation body")
			}
		})
	}
}

func TestInventoryLargeTaskIDsAndResponseOwnership(t *testing.T) {
	value := Row{"data": Row{"taskId": json.Number("9223372036854775807")}}
	want(t, inventoryTaskIDs("runninghub", value)[0], "9223372036854775807")
	wrong := inventoryPollValue("runninghub", "9223372036854775806", value)
	want(t, inventoryState("runninghub", wrong), "processing")
}

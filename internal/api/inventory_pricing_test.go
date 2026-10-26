package api

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
)

func TestInventoryExactPriceInheritance(t *testing.T) {
	a := setupIntegration(t)
	balance := jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id"))
	for _, scenario := range []string{"exact", "same_price", "conflict", "incompatible", "demo", "wrong_type", "unmatched", "disabled", "extras_conflict"} {
		t.Run(scenario, func(t *testing.T) {
			name := "inherit-" + scenario
			rid := "price-route-" + scenario
			protocol, kind, conditions := "openai", "image", "{}"
			if scenario == "demo" {
				protocol = "demo"
			}
			if scenario == "wrong_type" {
				kind = "video"
			}
			if scenario == "incompatible" {
				conditions = `{"duration":10}`
			}
			pid, mid := id(), id()
			a.Store.Exec("INSERT INTO providers(id,name,protocol,base_url,api_key,enabled,config) VALUES(?,?,?,'http://127.0.0.1:9','',1,'{}')", pid, name, protocol)
			sourceName := name
			if scenario == "unmatched" {
				sourceName += "-different"
			}
			a.Store.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,category,enabled,tags,parameters) VALUES(?,?,?,?,?,'','test',1,'[]','[]')", mid, sourceName, kind, pid, sourceName)
			a.Store.Exec("INSERT INTO prices(id,model_id,label,unit,credits,unit_price_micros,billing_extras,conditions,enabled) VALUES(?,?,'existing retail','request',12,123456,'{}',?,?)", id(), mid, conditions, scenario != "disabled")
			if scenario == "same_price" || scenario == "conflict" || scenario == "extras_conflict" {
				other := id()
				a.Store.Exec("INSERT INTO models(id,name,type,provider_id,upstream_model,description,category,enabled,tags,parameters) VALUES(?,?,'image',?,?,'','test',1,'[]','[]')", other, sourceName, pid, sourceName)
				micros, extras := 123456, "{}"
				if scenario == "conflict" {
					micros = 654321
				}
				if scenario == "extras_conflict" {
					extras = `{"imagePrice":0.5}`
				}
				a.Store.Exec("INSERT INTO prices(id,model_id,label,unit,credits,unit_price_micros,billing_extras,conditions) VALUES(?,?,'different label','request',12,?,?,'{}')", id(), other, micros, extras)
			}
			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			_ = w.Write([]string{"route_id", "provider", "model", "modality", "base_url", "api_key", "enabled", "upstream_model", "model_map"})
			_ = w.Write([]string{rid, "APIYI", name, "image", "http://127.0.0.1:9", "test-only-inheritance-key", "true", name, "{}"})
			w.Flush()
			report, err := a.ImportProviderInventory(strings.NewReader(buf.String()), true)
			if err != nil {
				t.Fatal(err)
			}
			target := inventoryModelID(rid)
			got := a.Store.All("SELECT * FROM prices WHERE model_id=?", target)
			if scenario == "exact" || scenario == "same_price" {
				want(t, len(got), 1)
				want(t, priceMicros(got[0]), int64(123456))
				want(t, report["pricesInherited"], 1)
				// Existing, even explicitly disabled, reviews must survive future imports.
				a.Store.Exec("UPDATE prices SET unit_price_micros=987654,enabled=0 WHERE model_id=?", target)
				if _, err := a.ImportProviderInventory(strings.NewReader(buf.String()), true); err != nil {
					t.Fatal(err)
				}
				saved := a.Store.One("SELECT * FROM prices WHERE model_id=?", target)
				want(t, priceMicros(saved), int64(987654))
				want(t, truth(saved["enabled"]), false)
			} else {
				want(t, len(got), 0)
				want(t, len(report["pricesPending"].([]Row)), 1)
			}
			want(t, truth(a.Store.One("SELECT enabled FROM models WHERE id=?", target)["enabled"]), false)
		})
	}
	want(t, jsonText(a.Store.All("SELECT id,balance FROM users ORDER BY id")), balance)
}

func TestInventoryCanonicalImageDimensions(t *testing.T) {
	for _, c := range []struct {
		resolution, ratio string
		w, h              int
	}{
		{"1k", "16:9", 1280, 720}, {"2k", "9:16", 1440, 2560},
		{"4k", "4:3", 3200, 2400}, {"4k", "1:1", 4096, 4096},
		{"720p", "16:9", 1280, 720}, {"1080p", "9:16", 1080, 1920},
	} {
		w, h := inventoryDimensions(c.resolution, c.ratio)
		want(t, w, c.w)
		want(t, h, c.h)
	}
}

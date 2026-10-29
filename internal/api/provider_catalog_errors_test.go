package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestVersionedSupplierCatalogBasePath(t *testing.T) {
	for _, protocol := range []string{"openvideo", "modelhub"} {
		t.Run(protocol, func(t *testing.T) {
			a := setupIntegration(t)
			mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v1/models" {
					http.NotFound(w, r)
					return
				}
				want(t, r.Header.Get("Authorization"), "Bearer openvideo-catalog-fixture")
				send(w, 200, Row{"models": []Row{{"id": "catalog-fixture-video", "type": "video"}}})
			}))
			defer mock.Close()
			server := httptest.NewServer(a.Handler())
			defer server.Close()
			c := &testClient{server: server.URL}
			c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
			p := object(c.ok(t, "POST", "/admin/providers", Row{"name": protocol + " fixture", "protocol": protocol, "base_url": mock.URL, "apiKey": "openvideo-catalog-fixture", "config": Row{"modelsPath": "/models"}}, 201))
			path := "/admin/providers/" + str(p["id"])
			err := object(c.ok(t, "POST", path+"/sync-models", Row{}, 502))
			if !strings.Contains(str(err["message"]), "404") {
				t.Fatalf("missing upstream status: %v", err)
			}
			p["base_url"] = mock.URL + "/v1"
			c.ok(t, "PUT", path, p, 200)
			want(t, object(c.ok(t, "POST", path+"/sync-models", Row{}, 200))["added"], 1)
			want(t, a.Store.One("SELECT type FROM provider_models WHERE provider_id=?", p["id"])["type"], "video")
			defaults := providerProtocol(protocol)
			if !strings.HasSuffix(str(defaults["baseURL"]), "/v1") {
				t.Fatal("supplier default base URL must include its API version prefix")
			}
			want(t, defaults["modelsPath"], "/models")
		})
	}
}

func TestProviderCatalogHTTPFailuresAreActionableAndRedacted(t *testing.T) {
	a := setupIntegration(t)
	var status atomic.Int32
	status.Store(401)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(status.Load()))
		fmt.Fprint(w, "sensitive-upstream-response "+r.Header.Get("Authorization"))
	}))
	defer mock.Close()
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	c := &testClient{server: server.URL}
	c.ok(t, "POST", "/auth/login", Row{"username": "test-admin", "password": "Test-admin-password-2026"}, 200)
	p := object(c.ok(t, "POST", "/admin/providers", Row{"name": "Error fixture", "protocol": "openai-image", "base_url": mock.URL, "apiKey": "catalog-error-fixture-key"}, 201))
	for _, tt := range []struct {
		status int
		hint   string
	}{{401, "密钥"}, {403, "权限"}, {404, "路径"}, {405, "GET"}, {429, "频率"}, {503, "上游服务"}} {
		t.Run(fmt.Sprint(tt.status), func(t *testing.T) {
			status.Store(int32(tt.status))
			body := object(c.ok(t, "POST", "/admin/providers/"+str(p["id"])+"/sync-models", Row{}, 502))
			message := str(body["message"])
			if !strings.Contains(message, fmt.Sprint(tt.status)) || !strings.Contains(message, tt.hint) {
				t.Fatalf("missing useful diagnosis: %s", message)
			}
			if strings.Contains(jsonText(body), "catalog-error-fixture-key") || strings.Contains(jsonText(body), "sensitive-upstream-response") {
				t.Fatal("upstream credentials or response body exposed")
			}
		})
	}
	want(t, a.Store.One("SELECT count(*) n FROM provider_models WHERE provider_id=?", p["id"])["n"], 0)
}

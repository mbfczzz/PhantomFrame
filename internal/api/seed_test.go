package api

import "testing"

func TestSeedWithoutDemoDoesNotRecreateFixtures(t *testing.T) {
	a := setupIntegration(t)
	a.Tx(func(s Store) {
		for _, table := range []string{"prices", "models", "provider_models", "providers", "users", "settings"} {
			s.Exec("DELETE FROM `" + table + "`")
		}
	})
	a.Config.Demo = false
	a.Seed()
	a.Seed() // Restart keeps paid/demo models empty and preserves the channel preset.
	for _, table := range []string{"prices", "models", "provider_models"} {
		want(t, a.Store.One("SELECT COUNT(*) n FROM `" + table + "`")["n"], 0)
	}
	want(t, a.Store.One("SELECT count(*) n FROM providers")["n"], 1)
	provider := a.Store.One("SELECT * FROM providers WHERE protocol='opencrow'")
	if provider == nil {
		t.Fatal("missing OpenCrow startup default")
	}
	want(t, provider["name"], "OpenCrow")
	want(t, provider["base_url"], "https://api.router.ai/v1")
	want(t, provider["api_key"], "")
	want(t, a.Store.One("SELECT COUNT(*) n FROM users")["n"], 1)
	want(t, a.Store.One("SELECT balance FROM users WHERE role='admin'")["balance"], 0)
	want(t, a.Store.Settings()["welcomeCredits"], "0")
}

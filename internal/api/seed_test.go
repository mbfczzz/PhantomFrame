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
	a.Seed() // Restart must also keep the catalog empty.
	for _, table := range []string{"prices", "models", "provider_models", "providers"} {
		want(t, a.Store.One("SELECT COUNT(*) n FROM `" + table + "`")["n"], 0)
	}
	want(t, a.Store.One("SELECT COUNT(*) n FROM users")["n"], 1)
	want(t, a.Store.One("SELECT balance FROM users WHERE role='admin'")["balance"], 0)
	want(t, a.Store.Settings()["welcomeCredits"], "0")
}

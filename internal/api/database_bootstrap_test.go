package api

import (
	"strings"
	"testing"
)

func TestDatabaseBootstrap(t *testing.T) {
	admin := setupIntegration(t)
	for _, suffix := range []string{"ordinary", "quoted`name"} {
		t.Run(suffix, func(t *testing.T) {
			c := admin.Config
			c.MySQLDatabase = "phantomframe_test_" + randomHex(8) + "_" + suffix
			quoted := "`" + strings.ReplaceAll(c.MySQLDatabase, "`", "``") + "`"
			t.Cleanup(func() { admin.Store.Exec("DROP DATABASE IF EXISTS " + quoted) })
			var count int
			if err := admin.DB.QueryRow("SELECT count(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", c.MySQLDatabase).Scan(&count); err != nil || count != 0 {
				t.Fatalf("test database must initially be absent: count=%d err=%v", count, err)
			}
			type result struct {
				app *App
				err error
			}
			opened := make(chan result, 2)
			for i := 0; i < 2; i++ {
				go func() {
					app, err := Open(c)
					opened <- result{app, err}
				}()
			}
			var app *App
			for i := 0; i < 2; i++ {
				r := <-opened
				if r.err != nil {
					t.Errorf("concurrent automatic database creation failed: %v", r.err)
					continue
				}
				t.Cleanup(r.app.Close)
				app = r.app
			}
			if t.Failed() {
				t.FailNow()
			}
			var selected, charset, collation string
			if err := app.DB.QueryRow("SELECT DATABASE()").Scan(&selected); err != nil {
				t.Fatal(err)
			}
			want(t, selected, c.MySQLDatabase)
			if err := app.DB.QueryRow("SELECT DEFAULT_CHARACTER_SET_NAME,DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", c.MySQLDatabase).Scan(&charset, &collation); err != nil {
				t.Fatal(err)
			}
			want(t, charset, "utf8mb4")
			want(t, collation, "utf8mb4_bin")
			if err := app.Migrate(); err != nil {
				t.Fatal(err)
			}
			app.Seed()
			app.Store.Exec("UPDATE users SET display_name='persisted-admin',balance=9876 WHERE username='test-admin'")
			reopened, err := Open(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(reopened.Close)
			if err := reopened.Migrate(); err != nil {
				t.Fatal(err)
			}
			reopened.Seed()
			u := reopened.Store.One("SELECT display_name,balance FROM users WHERE username='test-admin'")
			want(t, u["display_name"], "persisted-admin")
			want(t, u["balance"], 9876)
		})
	}
	t.Run("authentication failure does not create a database", func(t *testing.T) {
		c := admin.Config
		c.MySQLDatabase = "phantomframe_test_" + randomHex(8)
		t.Cleanup(func() { admin.Store.Exec("DROP DATABASE IF EXISTS `" + c.MySQLDatabase + "`") })
		c.MySQLPassword += "-incorrect"
		app, err := Open(c)
		if app != nil {
			app.Close()
		}
		if err == nil {
			t.Fatal("incorrect MySQL password must be rejected")
		}
		var count int
		if err := admin.DB.QueryRow("SELECT count(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", c.MySQLDatabase).Scan(&count); err != nil {
			t.Fatal(err)
		}
		want(t, count, 0)
	})
}

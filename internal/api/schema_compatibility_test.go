package api

import (
	"strings"
	"testing"
)

func TestMigrationRejectsForeignDatabaseBeforeWriting(t *testing.T) {
	a := setupIntegration(t)
	for _, foreign := range []struct{ table, ddl, insert, query string }{
		{
			"users",
			"CREATE TABLE users(id BIGINT PRIMARY KEY,username VARCHAR(64),password_hash VARCHAR(255))",
			"INSERT INTO users VALUES(1,'existing-account','keep-this-hash')",
			"SELECT password_hash FROM users WHERE id=1",
		},
		{
			"projects",
			"CREATE TABLE projects(id BIGINT PRIMARY KEY,owner_id BIGINT,name VARCHAR(255))",
			"INSERT INTO projects VALUES(1,1,'keep-this-project')",
			"SELECT name FROM projects WHERE id=1",
		},
	} {
		t.Run(foreign.table, func(t *testing.T) {
			c := a.Config
			c.MySQLDatabase = "phantomframe_test_" + randomHex(8)
			a.Store.Exec("CREATE DATABASE `" + c.MySQLDatabase + "` CHARACTER SET utf8mb4")
			t.Cleanup(func() { a.Store.Exec("DROP DATABASE `" + c.MySQLDatabase + "`") })
			other, err := Open(c)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			other.Store.Exec(foreign.ddl)
			other.Store.Exec(foreign.insert)
			var before string
			if err := other.DB.QueryRow(foreign.query).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if err := other.Migrate(); err == nil || !strings.Contains(err.Error(), "执行迁移前停止") {
				t.Fatalf("expected an incompatible database error before migration, got %v", err)
			}
			var count int
			if err := other.DB.QueryRow("SELECT count(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("migration wrote to the foreign database: got %d tables", count)
			}
			var after string
			if err := other.DB.QueryRow(foreign.query).Scan(&after); err != nil {
				t.Fatal(err)
			}
			want(t, after, before)
		})
	}
	if err := a.Migrate(); err != nil {
		t.Fatalf("existing PhantomFrame database must remain compatible: %v", err)
	}
}

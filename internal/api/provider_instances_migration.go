package api

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const providerInstancesMigration = "provider-instances-reset-v1-20261028"

// The user explicitly requested replacing the legacy model/provider catalog.
// Only catalog data is cleared, once. Tasks retain immutable execution snapshots.
func (a *App) migrateProviderInstances() error {
	if !a.Config.SupplierInstances {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	conn, err := a.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	lock := "pf-instance-migration:" + hash(a.Config.MySQLDatabase)[:32]
	var acquired int
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,60)", lock).Scan(&acquired); err != nil || acquired != 1 {
		return fmt.Errorf("无法获取供应商升级锁")
	}
	defer conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", lock)
	return try(func() {
		s := Store{conn}
		if s.One("SELECT name FROM migrations WHERE name=?", providerInstancesMigration) != nil {
			return
		}
		for _, row := range s.All("SELECT CONSTRAINT_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='jobs' AND COLUMN_NAME='model_id' AND REFERENCED_TABLE_NAME='models'") {
			name := strings.ReplaceAll(str(row["CONSTRAINT_NAME"]), "`", "``")
			s.Exec("ALTER TABLE jobs DROP FOREIGN KEY `" + name + "`")
		}
		tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		must(err)
		defer tx.Rollback()
		s = Store{tx}
		s.Exec("INSERT INTO invariant_locks(name) VALUES('provider_inventory') ON DUPLICATE KEY UPDATE name='provider_inventory'")
		s.All("SELECT id FROM providers ORDER BY id FOR UPDATE")
		s.All("SELECT id FROM models ORDER BY id FOR UPDATE")
		for _, j := range s.All("SELECT j.*,m.name old_name,m.type old_type,m.upstream_model old_upstream FROM jobs j LEFT JOIN models m ON m.id=j.model_id FOR UPDATE") {
			snapshot := object(j["provider_snapshot"])
			if str(snapshot["modelName"]) == "" {
				snapshot["modelName"] = first(j["old_name"], j["model_id"])
			}
			if str(snapshot["type"]) == "" {
				snapshot["type"] = first(j["old_type"], "image")
			}
			if str(snapshot["upstreamModel"]) == "" {
				snapshot["upstreamModel"] = first(j["old_upstream"], j["model_id"])
			}
			// In-flight tasks still need their submission credentials; completed
			// histories only need metadata, not retired account keys.
			if j["status"] == "queued" || j["status"] == "running" {
				snapshot = snapshotUploadProvider(s, snapshot)
			} else {
				delete(snapshot, "api_key")
				delete(snapshot, "upload_provider_snapshot")
			}
			s.Exec("UPDATE jobs SET provider_snapshot=? WHERE id=?", jsonText(snapshot), j["id"])
		}
		for _, table := range []string{"prices", "provider_models", "models", "inventory_routes", "providers", "inventory_deleted_providers"} {
			s.Exec("DELETE FROM `" + table + "`")
		}
		s.Exec("DELETE FROM settings WHERE `key`='_inventoryStartupFingerprint'")
		s.Exec("INSERT INTO migrations(name) VALUES(?)", providerInstancesMigration)
		must(tx.Commit())
	})
}

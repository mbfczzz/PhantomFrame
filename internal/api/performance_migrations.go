package api

import "github.com/go-sql-driver/mysql"

// Indexes match the WHERE + ORDER BY of list endpoints so MySQL can read rows
// in order instead of sorting every matching row on each poll. InnoDB appends
// the primary key (id), not seq, to secondary indexes, so seq is listed
// explicitly where queries break ties on it.
var performanceIndexes = []struct{ table, name, columns string }{
	{"jobs", "jobs_user_list", "user_id,hidden_at,created_at,seq"},
	{"jobs", "jobs_created", "created_at,seq"},
	{"jobs", "jobs_running_lease", "status,lease_until"},
	{"transactions", "transactions_user_list", "user_id,created_at,seq"},
	{"transactions", "transactions_kind", "kind,created_at"},
	{"transactions", "transactions_created", "created_at,seq"},
	{"assets", "assets_job", "job_id"},
	{"assets", "assets_user_list", "user_id,created_at,seq"},
	{"support_tickets", "tickets_user_list", "user_id,updated_at,seq"},
	{"support_tickets", "tickets_updated", "updated_at,seq"},
	{"audit_log", "audit_created", "created_at,id"},
	{"redeem_codes", "redeem_created", "created_at,id"},
}

var lifecycleColumns = []struct{ table, name, definition string }{
	// Per-job execution lease: recovery only reclaims jobs whose worker stopped
	// renewing, instead of every running job whenever leadership moves.
	{"jobs", "worker_id", "VARCHAR(64) NULL"},
	{"jobs", "lease_until", "DATETIME(3) NULL"},
	// API keys are bound to the account's credential version so password and
	// role changes revoke them together with web sessions.
	{"api_keys", "auth_version", "BIGINT NULL"},
}

func (a *App) ensureColumn(table, name, definition string) error {
	var count int
	if err := a.DB.QueryRow("SELECT count(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?", table, name).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if _, err := a.DB.Exec("ALTER TABLE `" + table + "` ADD COLUMN `" + name + "` " + definition); err != nil {
		if duplicate, ok := err.(*mysql.MySQLError); !ok || duplicate.Number != 1060 {
			return err
		}
	}
	return nil
}

func (a *App) ensureIndex(table, name, columns string) error {
	var count int
	if err := a.DB.QueryRow("SELECT count(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=?", table, name).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if _, err := a.DB.Exec("ALTER TABLE `" + table + "` ADD INDEX `" + name + "`(" + columns + ")"); err != nil {
		if duplicate, ok := err.(*mysql.MySQLError); !ok || duplicate.Number != 1061 {
			return err
		}
	}
	return nil
}

func (a *App) migratePerformanceAndLifecycle() error {
	for _, c := range lifecycleColumns {
		if err := a.ensureColumn(c.table, c.name, c.definition); err != nil {
			return err
		}
	}
	for _, index := range performanceIndexes {
		if err := a.ensureIndex(index.table, index.name, index.columns); err != nil {
			return err
		}
	}
	// Existing keys inherit the account's current version, so they keep working
	// until the next password or role change.
	_, err := a.DB.Exec("UPDATE api_keys k JOIN users u ON u.id=k.user_id SET k.auth_version=u.auth_version WHERE k.auth_version IS NULL")
	return err
}

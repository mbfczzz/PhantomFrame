package api

import (
	"context"
	"fmt"
	"time"
)

// Check shared table names before executing any DDL. Another application may use
// the same database name while having incompatible users or projects tables.
func (a *App) checkDatabaseCompatibility() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := a.DB.QueryContext(ctx, "SELECT TABLE_NAME,COLUMN_NAME,DATA_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME IN ('users','projects')")
	if err != nil {
		return fmt.Errorf("check database compatibility: %w", err)
	}
	defer rows.Close()
	tables := map[string]map[string]string{}
	for rows.Next() {
		var table, column, kind string
		if err := rows.Scan(&table, &column, &kind); err != nil {
			return err
		}
		if tables[table] == nil {
			tables[table] = map[string]string{}
		}
		tables[table][column] = kind
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, expected := range []struct{ table, column, kind string }{
		{"users", "id", "varchar"},
		{"users", "display_name", "varchar"},
		{"users", "password", "varchar"},
		{"users", "role", "varchar"},
		{"users", "balance", "bigint"},
		{"users", "auth_version", "bigint"},
		{"projects", "id", "varchar"},
		{"projects", "user_id", "varchar"},
		{"projects", "title", "varchar"},
		{"projects", "data", "json"},
	} {
		if columns := tables[expected.table]; columns != nil && columns[expected.column] != expected.kind {
			return fmt.Errorf("数据库 %q 的 %s 表与 PhantomFrame 不兼容（%s 应为 %s）；已在执行迁移前停止，请使用独立数据库，勿与其他项目共用业务表", a.Config.MySQLDatabase, expected.table, expected.column, expected.kind)
		}
	}
	return nil
}

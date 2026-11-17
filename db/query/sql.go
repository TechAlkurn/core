package query

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/TechAlkurn/core/cons"

	"github.com/TechAlkurn/core/config"

	"github.com/TechAlkurn/core/lib"
	"github.com/gin-gonic/gin"
)

type dBQuery struct {
	query string
}
type DBQuery interface {
	Query() (any, error)
	ResetDB() (any, error)
	ExportDB() (any, error)
	ImportDB(file string) (any, error)
}

func NewDBQuery(q any) DBQuery {
	query := strings.TrimSpace(lib.ToString(q))
	return dBQuery{
		query: query,
	}
}

// validTableIdent matches a bare MySQL identifier: letter or underscore,
// followed by letters, digits, or underscores. Schema-qualified names
// (schema.table) are handled by splitting on '.' and validating each part.
var validTableIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (q dBQuery) Query() (any, error) {
	sqlQuery := strings.TrimSpace(lib.ToString(q.query))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use SELECT for QueryContext
	// Use QueryContext
	valid := strings.HasPrefix(sqlQuery, "select") || strings.HasPrefix(sqlQuery, "show") || strings.HasPrefix(sqlQuery, "describe")
	valid = valid || strings.HasPrefix(sqlQuery, "SELECT") || strings.HasPrefix(sqlQuery, "SHOW") || strings.HasPrefix(sqlQuery, "DESCRIBE")
	if valid {
		hasLimit := regexp.MustCompile(`(?i)\blimit\s+\d+(\s*,\s*\d+|\s+offset\s+\d+)?\s*;?\s*$`)
		if strings.HasPrefix(sqlQuery, "select") && !hasLimit.MatchString(sqlQuery) {
			sqlQuery = strings.TrimSuffix(sqlQuery, ";") + " LIMIT 50"
		}
		rows, err := config.DB.ConnPool.QueryContext(ctx, sqlQuery)
		if err != nil {
			return nil, fmt.Errorf("error executing SELECT query: %v", err)
		}
		defer rows.Close()
		var results []map[string]any
		columns, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			values := make([]any, len(columns))
			valuePtrs := make([]any, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}
			if err := rows.Scan(valuePtrs...); err != nil {
				return nil, err
			}
			row := make(map[string]any)
			for i, col := range columns {
				val := values[i]
				// Convert []uint8 (aka []byte) to string if needed
				if b, ok := val.([]byte); ok {
					row[col] = string(b)
				} else {
					row[col] = val
				}
			}
			results = append(results, row)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if lib.IsEmpty(results) {
			return gin.H{"message": "No rows found"}, nil
		}
		return results, nil
	}

	// For UPDATE, DELETE, INSERT: use ExecContext
	result, err := config.DB.ConnPool.ExecContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("error executing query: %v", err)
	}
	affected, _ := result.RowsAffected()
	return gin.H{
		"query":   sqlQuery,
		"message": fmt.Sprintf("%d row(s) affected", affected),
	}, nil
}

// ResetTables truncates each of the comma-separated tables carried in
// q.query, with foreign-key checks disabled for the operation.
//
// Every statement runs on a single connection pinned via sql.DB.Conn so
// SET FOREIGN_KEY_CHECKS = 0/1 stays session-scoped and can't leak into
// the next borrower's queries. The re-enable is deferred, so even if
// TRUNCATE panics the pool connection is returned with FK checks back on.
//
// TRUNCATE is DDL (implicit COMMIT in MySQL) — it can't be rolled back,
// so partial failure on a multi-table list leaves earlier tables truncated.
// That's inherent to TRUNCATE, not something we can fix here.
//
// Example input: "migration,session"
func (q dBQuery) ExportDB() (any, error) {
	path, err := config.DumpDatabase(config.BackupOptions{})
	if err != nil {
		return nil, fmt.Errorf("pre-reset backup failed: %w", err)
	}
	config.Log.Printf("ExportDB: pre-reset backup written to %s", path)
	return gin.H{
		"message": fmt.Sprintf("Export path %s", path),
		"path":    path,
	}, nil
}

func (q dBQuery) ImportDB(file string) (any, error) {
	options := config.RestoreOptions{
		File: file,
	}
	path, err := config.RestoreDatabase(options)
	if err != nil {
		return nil, fmt.Errorf("import failed: %w", err)
	}
	config.Log.Printf("ImportDB: restore db from %s", path)
	return gin.H{
		"message": fmt.Sprintf("Import path %s", path),
		"path":    path,
	}, nil
}

func (q dBQuery) ResetDB() (any, error) {
	if mode := strings.ToLower(config.GetString("site_mode")); mode == "production" {
		return nil, fmt.Errorf("ResetDB is disabled in %s mode", mode)
	}
	path, err := config.DumpDatabase(config.BackupOptions{})
	if err != nil {
		return nil, fmt.Errorf("pre-reset backup failed: %w", err)
	}
	config.Log.Printf("ResetDBTables: pre-reset backup written to %s", path)
	list, err := NewDBQuery("SHOW TABLES").Query()
	if err != nil {
		return nil, fmt.Errorf("enumerate tables: %w", err)
	}
	tables := make([]string, 0)
	rows, ok := list.([]map[string]any)
	if !ok {
		return nil, errors.New("expected SHOW TABLES result set — got no rows or a different shape")
	}
	for _, row := range rows {
		// SHOW TABLES has exactly one column; grab whatever it's called.
		for _, v := range row {
			if name := lib.ToString(v); name != "" {
				tables = append(tables, name)
			}
			break
		}
	}
	sqlDB, err := config.DB.DB()
	if err != nil {
		return nil, fmt.Errorf("acquire *sql.DB: %w", err)
	}
	ctx := context.Background()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		return nil, fmt.Errorf("disable FK checks: %w", err)
	}
	// Re-enable on the SAME connection before it returns to the pool.
	// Deferred so a panic below still restores the session setting.
	defer func() {
		if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1"); err != nil {
			config.Log.Printf("ResetDBTables: failed to re-enable FK checks: %v", err)
		}
	}()

	// Rows with id / user_id ≤ preserveUserId are protected from wipe —
	// keeps the seeded ADMIN (1) and SYSTEM (2) accounts intact so the
	// app can still authenticate after a reset.
	protected := []string{
		"GenerateOTP", "user", "auth_assignment", "auth_item", "auth_item_child", "auth_rule", "core_acl", "core_cities", "core_cms_block", "core_cms_block_mapper", "core_cms_category", "core_cms_category_path", "core_cms_followers", "core_cms_item", "core_cms_item_category_mapping", "core_cms_item_lg", "core_cms_item_mapper", "core_cms_item_path", "core_cms_keyword", "core_cms_layout", "core_cms_link_preview", "core_cms_relation", "core_cms_widget", "core_cms_widget_mapper", "core_config", "core_config_group", "core_config_option", "core_config_value", "core_contact", "core_countries", "core_cron", "core_field", "core_field_item", "core_field_option", "core_field_rule", "core_field_section", "core_field_section_mapping", "core_field_value", "core_holidays", "core_language", "core_mail", "core_mail_attachment", "core_mail_recipients", "core_mail_template", "core_mail_template_layout", "core_mail_template_layout_lg", "core_mail_template_lg", "core_media", "core_media_item", "core_module", "core_newsletter", "core_notification_action", "core_notification_action_lg", "core_plan", "core_seo", "core_seo_option", "core_states", "core_tags", "core_tags_type", "core_timezone", "core_translate", "user_catalog_category", "user_catalog_category_path",
		"user_catalog_tag", "user_catalog_tag_type",
	}

	if tbls := config.GetString("tables_not_allowed_to_reset"); lib.NotEmpty(tbls) {
		protected := append(protected, strings.Split(tbls, ",")...)
		for i := range protected {
			protected[i] = strings.TrimSpace(protected[i])
		}
	}
	resetTables := make([]string, 0)
	for _, tbl := range tables {
		// Per-table branches use the CLEAN (unquoted) name for matching,
		// then reintroduce backticks via quoteIdent when interpolating
		// the SQL. Any table not listed below gets a plain TRUNCATE.
		if len(protected) > 0 && lib.InArray(tbl, protected) {
			continue
		}
		resetTables = append(resetTables, tbl)
		switch tbl {
		case "user_profile":
			if _, err := conn.ExecContext(ctx, "DELETE FROM `user` WHERE id > ?", cons.ADMIN_ID); err != nil {
				return nil, fmt.Errorf("delete from `user`: %w", err)
			}
			if _, err := conn.ExecContext(ctx, fmt.Sprintf("ALTER TABLE `user` AUTO_INCREMENT=%d", cons.ADMIN_ID+1)); err != nil {
				return nil, fmt.Errorf("reset auto_increment for `user`: %w", err)
			}
			if _, err := conn.ExecContext(ctx, "DELETE FROM `core_media_item` WHERE user_id > ?", cons.ADMIN_ID); err != nil {
				return nil, fmt.Errorf("delete from `core_media_item`: %w", err)
			}
			if _, err := conn.ExecContext(ctx, "DELETE FROM `user_profile` WHERE user_id > ?", cons.ADMIN_ID); err != nil {
				return nil, fmt.Errorf("delete from `user_profile`: %w", err)
			}
			if _, err := conn.ExecContext(ctx, "DELETE FROM `auth_assignment` WHERE user_id > ?", cons.ADMIN_ID); err != nil {
				return nil, fmt.Errorf("delete from `auth_assignment`: %w", err)
			}
		default:
			if _, err := conn.ExecContext(ctx, "TRUNCATE TABLE "+quoteIdent(tbl)); err != nil {
				return nil, fmt.Errorf("truncate table `%s`: %w", tbl, err)
			}
		}
	}
	return gin.H{
		"message":   fmt.Sprintf("Reset %d tables, skipped %d", len(resetTables), len(protected)),
		"reset":     resetTables,
		"protected": protected,
	}, nil
}

// quoteIdent backtick-quotes a possibly-schema-qualified identifier for
// safe interpolation into raw SQL (e.g. `schema`.`table`). Input must have
// already been validated by sanitizeTableList — this function assumes each
// segment is a valid identifier and does not re-check.
func quoteIdent(name string) string {
	segs := strings.Split(name, ".")
	for i, s := range segs {
		segs[i] = "`" + s + "`"
	}
	return strings.Join(segs, ".")
}

// sanitizeTableList takes a comma-separated table list (`t1,t2`,
// “ `t1`,`t2` “, or `schema.t1,schema.t2`), strips any surrounding
// backticks, validates each identifier, and returns the clean (unquoted)
// names as a slice. Callers backtick-quote themselves when interpolating
// — keeping names unquoted lets per-name equality checks (`if name ==
// "user"`) work as expected. Returns an error on the first bad name —
// no partial output.
func sanitizeTableList(input string) ([]string, error) {
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	for _, raw := range parts {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		segs := strings.Split(name, ".")
		clean := make([]string, 0, len(segs))
		for _, seg := range segs {
			seg = strings.Trim(strings.TrimSpace(seg), "`")
			if !validTableIdent.MatchString(seg) {
				return nil, fmt.Errorf("invalid table name: %q", raw)
			}
			clean = append(clean, seg)
		}
		out = append(out, strings.Join(clean, "."))
	}
	if len(out) == 0 {
		return nil, errors.New("no tables specified")
	}
	return out, nil
}

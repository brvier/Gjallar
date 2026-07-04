package check

import (
	"context"
	"database/sql"
	"fmt"

	"gjallar/internal/config"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx"
	_ "github.com/sijms/go-ora/v2"     // database/sql driver "oracle"
)

// sqlCheck covers both postgres and oracle through database/sql.
// It deliberately opens a fresh connection per check: a monitoring probe
// should exercise connect/auth every time, and this avoids idle-pool tuning.
type sqlCheck struct {
	driver string
	dsn    string
	query  string
	rule   *Rule
}

func newSQLCheck(driver string, m config.Monitor) (*sqlCheck, error) {
	rule, err := ParseRule(m.Rule)
	if err != nil {
		return nil, err
	}
	return &sqlCheck{driver: driver, dsn: m.DSN, query: m.Query, rule: rule}, nil
}

func (c *sqlCheck) Check(ctx context.Context) (bool, string) {
	db, err := sql.Open(c.driver, c.dsn)
	if err != nil {
		return false, fmt.Sprintf("open: %v", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, c.query)
	if err != nil {
		return false, fmt.Sprintf("query: %v", err)
	}
	defer rows.Close()

	var first string
	var hasFirst bool
	count := 0
	for rows.Next() {
		if count == 0 {
			var v any
			if err := rows.Scan(&v); err != nil {
				return false, fmt.Sprintf("scan: %v", err)
			}
			first, hasFirst = renderValue(v), true
		}
		count++
		if count > 0 && !c.rule.TargetsRows() {
			break // only the first row matters for value rules
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Sprintf("rows: %v", err)
	}

	if c.rule.TargetsRows() {
		if err := c.rule.EvalRows(count); err != nil {
			return false, err.Error()
		}
		return true, ""
	}
	if !hasFirst {
		return false, fmt.Sprintf("query returned no rows (rule %q)", c.rule.raw)
	}
	if err := c.rule.EvalValue(first); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func renderValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

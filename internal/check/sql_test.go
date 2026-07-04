package check

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gjallar/internal/config"

	_ "modernc.org/sqlite" // exercises the generic checker without a server
)

// sqliteCheck builds a sqlCheck against a local SQLite file: the generic
// database/sql logic is identical for postgres and oracle.
func sqliteCheck(t *testing.T, query, rule string) *sqlCheck {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "t.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE jobs (id INTEGER, status TEXT);
		INSERT INTO jobs VALUES (1, 'ok'), (2, 'ok'), (3, 'stuck')`); err != nil {
		t.Fatal(err)
	}
	c, err := newSQLCheck("sqlite", config.Monitor{DSN: dsn, Query: query, Rule: rule})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSQLCheck(t *testing.T) {
	cases := []struct {
		name, query, rule string
		ok                bool
		wantMsg           string
	}{
		{"value equals", `SELECT count(*) FROM jobs WHERE status = 'stuck'`, "== 1", true, ""},
		{"value fails", `SELECT count(*) FROM jobs WHERE status = 'stuck'`, "== 0", false, "does not satisfy"},
		{"numeric compare", `SELECT count(*) FROM jobs`, "> 2", true, ""},
		{"regex on text", `SELECT status FROM jobs WHERE id = 1`, "~ ^ok$", true, ""},
		{"rows rule", `SELECT id FROM jobs WHERE status = 'ok'`, "rows == 2", true, ""},
		{"rows rule fails", `SELECT id FROM jobs WHERE status = 'gone'`, "rows > 0", false, "row count 0"},
		{"no rows for value rule", `SELECT id FROM jobs WHERE status = 'gone'`, "== 1", false, "no rows"},
		{"query error", `SELECT nope FROM missing`, "== 1", false, "query:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := sqliteCheck(t, tc.query, tc.rule)
			ok, msg := c.Check(context.Background())
			if ok != tc.ok {
				t.Fatalf("ok = %v, msg = %q", ok, msg)
			}
			if tc.wantMsg != "" && !strings.Contains(msg, tc.wantMsg) {
				t.Errorf("msg %q does not contain %q", msg, tc.wantMsg)
			}
		})
	}
}

func TestSQLCheckBadRule(t *testing.T) {
	if _, err := newSQLCheck("sqlite", config.Monitor{DSN: "x", Query: "SELECT 1", Rule: "=> 1"}); err == nil {
		t.Error("expected rule parse error")
	}
}

// Integration tests, gated by env vars:
//
//	docker run --rm -e POSTGRES_PASSWORD=secret -p 5432:5432 postgres:16
//	GJALLAR_TEST_PG_DSN="postgres://postgres:secret@localhost:5432/postgres?sslmode=disable" go test ./internal/check/
//
//	docker run --rm -p 1521:1521 -e ORACLE_PASSWORD=secret gvenzl/oracle-free:23-slim
//	GJALLAR_TEST_ORA_DSN="oracle://system:secret@localhost:1521/FREEPDB1" go test ./internal/check/
func TestPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("GJALLAR_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("GJALLAR_TEST_PG_DSN not set")
	}
	c, err := newSQLCheck("pgx", config.Monitor{DSN: dsn, Query: "SELECT 1", Rule: "== 1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if ok, msg := c.Check(ctx); !ok {
		t.Errorf("postgres check failed: %s", msg)
	}
}

func TestOracleIntegration(t *testing.T) {
	dsn := os.Getenv("GJALLAR_TEST_ORA_DSN")
	if dsn == "" {
		t.Skip("GJALLAR_TEST_ORA_DSN not set")
	}
	c, err := newSQLCheck("oracle", config.Monitor{DSN: dsn, Query: "SELECT status FROM v$instance", Rule: "~ OPEN"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if ok, msg := c.Check(ctx); !ok {
		t.Errorf("oracle check failed: %s", msg)
	}
}

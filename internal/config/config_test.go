package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gjallar.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validConfig = `
listen: ":9090"
alerts:
  tg:
    url: "telegram://token@telegram?chats=1"
  sms:
    type: freemobile
    user: "123"
    pass: "abc"
defaults:
  interval: 30s
  alerts: [tg]
monitors:
  - name: web
    type: http
    url: "https://example.com"
    expect_status: 200
    body_regex: 'ok'
  - name: db
    type: postgres
    dsn: "postgres://u:p@h/db"
    query: "SELECT 1"
    rule: "== 1"
    interval: 2m
    alerts: [sms]
  - name: gw
    type: ping
    host: "127.0.0.1"
  - name: prom
    type: prometheus
    url: "http://h:9100/metrics"
    metric: "up"
    rule: "> 0"
  - name: ora
    type: oracle
    dsn: "oracle://u:p@h:1521/svc"
    query: "SELECT status FROM v$instance"
    rule: "~ OPEN"
`

func TestLoadValid(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != ":9090" {
		t.Errorf("Listen = %q", cfg.Listen)
	}
	if cfg.Database != "gjallar.db" {
		t.Errorf("Database default = %q", cfg.Database)
	}
	if cfg.Retention.D() != 720*time.Hour {
		t.Errorf("Retention default = %v", cfg.Retention.D())
	}
	web := cfg.Monitors[0]
	if web.Interval.D() != 30*time.Second {
		t.Errorf("web interval (from defaults) = %v", web.Interval.D())
	}
	if web.Timeout.D() != 10*time.Second {
		t.Errorf("web timeout (builtin default) = %v", web.Timeout.D())
	}
	if web.FailureThreshold != 3 {
		t.Errorf("web failure_threshold = %d", web.FailureThreshold)
	}
	if len(web.Alerts) != 1 || web.Alerts[0] != "tg" {
		t.Errorf("web alerts (from defaults) = %v", web.Alerts)
	}
	if web.Method != "GET" {
		t.Errorf("web method default = %q", web.Method)
	}
	db := cfg.Monitors[1]
	if db.Interval.D() != 2*time.Minute {
		t.Errorf("db interval = %v", db.Interval.D())
	}
	if len(db.Alerts) != 1 || db.Alerts[0] != "sms" {
		t.Errorf("db alerts = %v", db.Alerts)
	}
	if cfg.Monitors[2].Count != 3 {
		t.Errorf("ping count default = %d", cfg.Monitors[2].Count)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name, yaml, wantErr string
	}{
		{"no monitors", `listen: ":1"`, "no monitors"},
		{"unknown field", "monitors:\n  - name: x\n    type: http\n    url: h\n    bogus: 1", "bogus"},
		{"unknown type", "monitors:\n  - name: x\n    type: ftp", `unknown type "ftp"`},
		{"duplicate name", "monitors:\n  - name: x\n    type: ping\n    host: h\n  - name: x\n    type: ping\n    host: h", "duplicate name"},
		{"missing url", "monitors:\n  - name: x\n    type: http", "url is required"},
		{"bad regex", "monitors:\n  - name: x\n    type: http\n    url: h\n    body_regex: '['", "body_regex"},
		{"missing dsn", "monitors:\n  - name: x\n    type: postgres\n    query: q\n    rule: '== 1'", "dsn is required"},
		{"missing rule", "monitors:\n  - name: x\n    type: prometheus\n    url: h\n    metric: up", "rule is required"},
		{"unknown alert ref", "monitors:\n  - name: x\n    type: ping\n    host: h\n    alerts: [nope]", `unknown alert "nope"`},
		{"alert missing url", "alerts:\n  a: {}\nmonitors:\n  - name: x\n    type: ping\n    host: h", "url is required"},
		{"freemobile missing pass", "alerts:\n  a:\n    type: freemobile\n    user: u\nmonitors:\n  - name: x\n    type: ping\n    host: h", "user and pass"},
		{"bad duration", "monitors:\n  - name: x\n    type: ping\n    host: h\n    interval: fast", "invalid duration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

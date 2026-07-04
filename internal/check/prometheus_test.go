package check

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gjallar/internal/config"
)

const metricsFixture = `# HELP node_filesystem_avail_bytes Available bytes.
# TYPE node_filesystem_avail_bytes gauge
node_filesystem_avail_bytes{device="/dev/sda1",mountpoint="/"} 6e9
node_filesystem_avail_bytes{device="/dev/sdb1",mountpoint="/data"} 1e9
# HELP http_requests_total Total requests.
# TYPE http_requests_total counter
http_requests_total{code="200"} 1027
# TYPE up untyped
up 1
`

func promMonitor(url, metric, rule string, labels map[string]string) config.Monitor {
	return config.Monitor{
		Name: "t", Type: "prometheus", URL: url, Metric: metric, Rule: rule,
		Labels: labels, Timeout: config.Duration(5 * time.Second),
	}
}

func TestPromCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(metricsFixture))
	}))
	defer srv.Close()

	cases := []struct {
		name    string
		metric  string
		rule    string
		labels  map[string]string
		ok      bool
		wantMsg string
	}{
		{"gauge with label match", "node_filesystem_avail_bytes", "> 5e9", map[string]string{"mountpoint": "/"}, true, ""},
		{"rule must hold for all series", "node_filesystem_avail_bytes", "> 5e9", nil, false, "does not satisfy"},
		{"counter", "http_requests_total", "> 1000", nil, true, ""},
		{"untyped", "up", "== 1", nil, true, ""},
		{"untyped failing", "up", "== 0", nil, false, "does not satisfy"},
		{"unknown metric", "nope_metric", "> 0", nil, false, "not found"},
		{"no series matches labels", "up", "> 0", map[string]string{"job": "x"}, false, "no series"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := newPromCheck(promMonitor(srv.URL, tc.metric, tc.rule, tc.labels))
			if err != nil {
				t.Fatal(err)
			}
			ok, msg := c.Check(context.Background())
			if ok != tc.ok {
				t.Fatalf("ok = %v, msg = %q", ok, msg)
			}
			if tc.wantMsg != "" && !strings.Contains(msg, tc.wantMsg) {
				t.Errorf("msg %q does not contain %q", msg, tc.wantMsg)
			}
		})
	}

	t.Run("rows rule rejected at build time", func(t *testing.T) {
		if _, err := newPromCheck(promMonitor(srv.URL, "up", "rows > 0", nil)); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("http error", func(t *testing.T) {
		c, _ := newPromCheck(promMonitor("http://127.0.0.1:1/metrics", "up", "== 1", nil))
		if ok, _ := c.Check(context.Background()); ok {
			t.Error("expected failure")
		}
	})
}

package check

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gjallar/internal/config"
)

func esMonitor(url, rule string) config.Monitor {
	return config.Monitor{
		Name: "t", Type: "elasticsearch", URL: url, Index: "broadcasts",
		TimestampField: "start_date", Rule: rule,
		Timeout: config.Duration(5 * time.Second),
	}
}

// esServer returns a fake ES that reports max(start_date) = now - lag.
func esServer(t *testing.T, lag time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/broadcasts/_search") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		ms := time.Now().Add(-lag).UnixMilli()
		fmt.Fprintf(w, `{"aggregations":{"max_ts":{"value":%d.0}}}`, ms)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestESCheckFresh(t *testing.T) {
	srv := esServer(t, 30*time.Minute) // 0.5h lag
	c, _ := newESCheck(esMonitor(srv.URL, "< 3"))
	if ok, msg := c.Check(context.Background()); !ok {
		t.Errorf("expected ok, got %q", msg)
	}
}

func TestESCheckStale(t *testing.T) {
	srv := esServer(t, 13*time.Hour) // the incident: 13h lag
	c, _ := newESCheck(esMonitor(srv.URL, "< 3"))
	ok, msg := c.Check(context.Background())
	if ok || !strings.Contains(msg, "freshness lag") {
		t.Errorf("got ok=%v msg=%q", ok, msg)
	}
}

func TestESCheckEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"aggregations":{"max_ts":{"value":null}}}`)
	}))
	defer srv.Close()
	c, _ := newESCheck(esMonitor(srv.URL, "< 3"))
	if ok, msg := c.Check(context.Background()); ok || !strings.Contains(msg, "empty index") {
		t.Errorf("got ok=%v msg=%q", ok, msg)
	}
}

func TestESCheckDown(t *testing.T) {
	c, _ := newESCheck(esMonitor("http://127.0.0.1:1", "< 3"))
	if ok, _ := c.Check(context.Background()); ok {
		t.Error("expected failure")
	}
}

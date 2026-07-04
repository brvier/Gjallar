package alert

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gjallar/internal/check"
	"gjallar/internal/config"
	"gjallar/internal/store"
)

type fakeNotifier struct {
	mu    sync.Mutex
	sent  []string
	waitc chan struct{}
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{waitc: make(chan struct{}, 16)}
}

func (f *fakeNotifier) Send(ctx context.Context, title, message string) error {
	f.mu.Lock()
	f.sent = append(f.sent, title+" | "+message)
	f.mu.Unlock()
	f.waitc <- struct{}{}
	return nil
}

// waitSent waits for n total sends (they happen on goroutines).
func (f *fakeNotifier) waitSent(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		f.mu.Lock()
		got := len(f.sent)
		f.mu.Unlock()
		if got >= n {
			f.mu.Lock()
			defer f.mu.Unlock()
			return append([]string(nil), f.sent...)
		}
		select {
		case <-f.waitc:
		case <-deadline:
			t.Fatalf("timed out waiting for %d sends, got %d", n, got)
		}
	}
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func testSetup(t *testing.T) (*store.Store, *config.Config, *fakeNotifier) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{
		Monitors: []config.Monitor{
			{Name: "web", Type: "ping", Host: "h", FailureThreshold: 3, Alerts: []string{"fake"}},
		},
	}
	return st, cfg, newFakeNotifier()
}

func result(ok bool, msg string) check.Result {
	return check.Result{Monitor: "web", Time: time.Now(), OK: ok, Message: msg}
}

func TestThresholdAndRecovery(t *testing.T) {
	st, cfg, fake := testSetup(t)
	e, err := NewEngine(cfg, st, map[string]Notifier{"fake": fake})
	if err != nil {
		t.Fatal(err)
	}

	// Two failures: below threshold, no alert, no incident.
	e.Process(result(false, "boom"))
	e.Process(result(false, "boom"))
	if fake.count() != 0 {
		t.Fatalf("premature alert: %v", fake.sent)
	}
	if open, _ := st.HasOpenIncident("web"); open {
		t.Fatal("premature incident")
	}

	// Third failure crosses the threshold.
	e.Process(result(false, "boom"))
	sent := fake.waitSent(t, 1)
	if !strings.Contains(sent[0], "DOWN: web") || !strings.Contains(sent[0], "boom") {
		t.Errorf("down alert = %q", sent[0])
	}
	if open, _ := st.HasOpenIncident("web"); !open {
		t.Fatal("expected incident")
	}

	// Still failing: no repeat alert.
	e.Process(result(false, "boom"))
	if fake.count() != 1 {
		t.Fatalf("repeat alert: %v", fake.sent)
	}

	// Recovery.
	e.Process(result(true, ""))
	sent = fake.waitSent(t, 2)
	if !strings.Contains(sent[1], "UP: web") || !strings.Contains(sent[1], "recovered") {
		t.Errorf("up alert = %q", sent[1])
	}
	if open, _ := st.HasOpenIncident("web"); open {
		t.Fatal("incident should be resolved")
	}
}

func TestFlappingBelowThreshold(t *testing.T) {
	st, cfg, fake := testSetup(t)
	e, _ := NewEngine(cfg, st, map[string]Notifier{"fake": fake})
	for i := 0; i < 5; i++ {
		e.Process(result(false, "blip"))
		e.Process(result(false, "blip"))
		e.Process(result(true, "")) // recovers before the 3rd failure
	}
	if fake.count() != 0 {
		t.Errorf("flapping below threshold alerted: %v", fake.sent)
	}
}

func TestRestartWithOpenIncident(t *testing.T) {
	st, cfg, fake := testSetup(t)
	if err := st.OpenIncident("web", "pre-restart", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	e, err := NewEngine(cfg, st, map[string]Notifier{"fake": fake})
	if err != nil {
		t.Fatal(err)
	}
	// Still failing after restart: no duplicate DOWN alert.
	e.Process(result(false, "still down"))
	if fake.count() != 0 {
		t.Fatalf("duplicate DOWN after restart: %v", fake.sent)
	}
	// Recovery after restart fires UP and resolves the pre-restart incident.
	e.Process(result(true, ""))
	sent := fake.waitSent(t, 1)
	if !strings.Contains(sent[0], "UP: web") {
		t.Errorf("alert = %q", sent[0])
	}
	if open, _ := st.HasOpenIncident("web"); open {
		t.Fatal("incident should be resolved")
	}
}

func TestFreeMobile(t *testing.T) {
	var gotQuery url.Values
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(status)
	}))
	defer srv.Close()

	fm := NewFreeMobile("123", "key")
	fm.BaseURL = srv.URL

	if err := fm.Send(context.Background(), "[Gjallar] DOWN: web", "web — boom"); err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("user") != "123" || gotQuery.Get("pass") != "key" {
		t.Errorf("credentials = %v", gotQuery)
	}
	if msg := gotQuery.Get("msg"); !strings.Contains(msg, "DOWN: web") || !strings.Contains(msg, "boom") {
		t.Errorf("msg = %q", msg)
	}

	for code, want := range map[int]string{
		http.StatusPaymentRequired:     "quota",
		http.StatusForbidden:           "credentials",
		http.StatusInternalServerError: "server error",
	} {
		status = code
		err := fm.Send(context.Background(), "t", "m")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("status %d: err = %v, want %q", code, err, want)
		}
	}
}

func TestBuildNotifiers(t *testing.T) {
	ns, err := BuildNotifiers(map[string]config.Alert{
		"sms": {Type: "freemobile", User: "u", Pass: "p"},
	})
	if err != nil || ns["sms"] == nil {
		t.Fatalf("BuildNotifiers: %v", err)
	}
	if _, err := BuildNotifiers(map[string]config.Alert{
		"bad": {URL: "not-a-shoutrrr-url"},
	}); err == nil {
		t.Fatal("expected error for invalid shoutrrr URL")
	}
}

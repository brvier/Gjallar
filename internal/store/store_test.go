package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestResults(t *testing.T) {
	s := openTemp(t)
	now := time.Now()
	for i := 0; i < 5; i++ {
		ok := i != 2
		msg := ""
		if !ok {
			msg = "boom"
		}
		if err := s.InsertResult("web", now.Add(time.Duration(i)*time.Second), ok, 42*time.Millisecond, msg); err != nil {
			t.Fatal(err)
		}
	}
	s.InsertResult("other", now, true, time.Millisecond, "")

	rows, err := s.RecentResults("web", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows", len(rows))
	}
	if !rows[0].Time.After(rows[1].Time) {
		t.Error("expected newest first")
	}
	if rows[0].Latency != 42*time.Millisecond {
		t.Errorf("latency = %v", rows[0].Latency)
	}

	up, count, err := s.UptimeSince("web", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 || up != 0.8 {
		t.Errorf("uptime = %v over %d checks, want 0.8 over 5", up, count)
	}

	// No results yet for an unknown monitor: zero count, no error.
	_, count, err = s.UptimeSince("ghost", now.Add(-time.Hour))
	if err != nil || count != 0 {
		t.Errorf("ghost: count=%d err=%v", count, err)
	}
}

func TestIncidents(t *testing.T) {
	s := openTemp(t)
	start := time.Now().Add(-10 * time.Minute)

	if open, _ := s.HasOpenIncident("web"); open {
		t.Fatal("no incident expected yet")
	}
	if err := s.OpenIncident("web", "connection refused", start); err != nil {
		t.Fatal(err)
	}
	if open, _ := s.HasOpenIncident("web"); !open {
		t.Fatal("expected open incident")
	}

	incs, err := s.Incidents(10)
	if err != nil || len(incs) != 1 {
		t.Fatalf("incidents: %v, %v", incs, err)
	}
	if !incs[0].Open() || incs[0].Reason != "connection refused" {
		t.Errorf("incident = %+v", incs[0])
	}

	if err := s.ResolveIncident("web", start.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if open, _ := s.HasOpenIncident("web"); open {
		t.Fatal("expected incident resolved")
	}
	incs, _ = s.MonitorIncidents("web", 10)
	if len(incs) != 1 || incs[0].Open() {
		t.Fatalf("incidents after resolve: %+v", incs)
	}
	if d := incs[0].Duration(); d != 5*time.Minute {
		t.Errorf("duration = %v", d)
	}
}

func TestPrune(t *testing.T) {
	s := openTemp(t)
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now()

	s.InsertResult("web", old, true, time.Millisecond, "")
	s.InsertResult("web", recent, true, time.Millisecond, "")
	s.OpenIncident("web", "old", old)
	s.ResolveIncident("web", old.Add(time.Minute))
	s.OpenIncident("web", "still open", old) // open incidents survive pruning

	if err := s.Prune(time.Now().Add(-24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.RecentResults("web", 10)
	if len(rows) != 1 {
		t.Errorf("results after prune = %d, want 1", len(rows))
	}
	incs, _ := s.Incidents(10)
	if len(incs) != 1 || !incs[0].Open() {
		t.Errorf("incidents after prune = %+v, want the open one", incs)
	}
}

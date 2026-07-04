// Package store persists check results and incidents in SQLite.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go database/sql driver "sqlite"
)

type Store struct {
	db *sql.DB
}

type ResultRow struct {
	Time    time.Time
	OK      bool
	Latency time.Duration
	Message string
}

type Incident struct {
	ID         int64
	Monitor    string
	StartedAt  time.Time
	ResolvedAt time.Time // zero while open
	Reason     string
}

func (i Incident) Open() bool { return i.ResolvedAt.IsZero() }

func (i Incident) Duration() time.Duration {
	if i.Open() {
		return time.Since(i.StartedAt)
	}
	return i.ResolvedAt.Sub(i.StartedAt)
}

const schema = `
CREATE TABLE IF NOT EXISTS results (
    id         INTEGER PRIMARY KEY,
    monitor    TEXT    NOT NULL,
    checked_at INTEGER NOT NULL,
    ok         INTEGER NOT NULL,
    latency_ms INTEGER NOT NULL,
    message    TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_results_monitor_time ON results(monitor, checked_at DESC);

CREATE TABLE IF NOT EXISTS incidents (
    id          INTEGER PRIMARY KEY,
    monitor     TEXT    NOT NULL,
    started_at  INTEGER NOT NULL,
    resolved_at INTEGER,
    reason      TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_incidents_monitor ON incidents(monitor, started_at DESC);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	// All writes go through the single pipeline consumer; one connection
	// keeps SQLite locking trivial.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) InsertResult(monitor string, at time.Time, ok bool, latency time.Duration, msg string) error {
	_, err := s.db.Exec(`INSERT INTO results (monitor, checked_at, ok, latency_ms, message) VALUES (?, ?, ?, ?, ?)`,
		monitor, at.UnixMilli(), boolToInt(ok), latency.Milliseconds(), msg)
	return err
}

// RecentResults returns the last n results, newest first.
func (s *Store) RecentResults(monitor string, n int) ([]ResultRow, error) {
	rows, err := s.db.Query(`SELECT checked_at, ok, latency_ms, message FROM results
		WHERE monitor = ? ORDER BY checked_at DESC LIMIT ?`, monitor, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResultRow
	for rows.Next() {
		var at, latency int64
		var ok int
		var r ResultRow
		if err := rows.Scan(&at, &ok, &latency, &r.Message); err != nil {
			return nil, err
		}
		r.Time = time.UnixMilli(at)
		r.OK = ok == 1
		r.Latency = time.Duration(latency) * time.Millisecond
		out = append(out, r)
	}
	return out, rows.Err()
}

// UptimeSince returns the fraction of successful checks and the total count
// since the given time.
func (s *Store) UptimeSince(monitor string, since time.Time) (float64, int, error) {
	var avg sql.NullFloat64
	var count int
	err := s.db.QueryRow(`SELECT AVG(ok), COUNT(*) FROM results WHERE monitor = ? AND checked_at >= ?`,
		monitor, since.UnixMilli()).Scan(&avg, &count)
	return avg.Float64, count, err
}

func (s *Store) OpenIncident(monitor, reason string, at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO incidents (monitor, started_at, reason) VALUES (?, ?, ?)`,
		monitor, at.UnixMilli(), reason)
	return err
}

func (s *Store) ResolveIncident(monitor string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE incidents SET resolved_at = ? WHERE monitor = ? AND resolved_at IS NULL`,
		at.UnixMilli(), monitor)
	return err
}

func (s *Store) HasOpenIncident(monitor string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM incidents WHERE monitor = ? AND resolved_at IS NULL`,
		monitor).Scan(&n)
	return n > 0, err
}

// Incidents returns the most recent incidents (open and resolved), newest first.
func (s *Store) Incidents(limit int) ([]Incident, error) {
	rows, err := s.db.Query(`SELECT id, monitor, started_at, resolved_at, reason FROM incidents
		ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		var inc Incident
		var started int64
		var resolved sql.NullInt64
		if err := rows.Scan(&inc.ID, &inc.Monitor, &started, &resolved, &inc.Reason); err != nil {
			return nil, err
		}
		inc.StartedAt = time.UnixMilli(started)
		if resolved.Valid {
			inc.ResolvedAt = time.UnixMilli(resolved.Int64)
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

// MonitorIncidents returns recent incidents for one monitor, newest first.
func (s *Store) MonitorIncidents(monitor string, limit int) ([]Incident, error) {
	rows, err := s.db.Query(`SELECT id, monitor, started_at, resolved_at, reason FROM incidents
		WHERE monitor = ? ORDER BY started_at DESC LIMIT ?`, monitor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		var inc Incident
		var started int64
		var resolved sql.NullInt64
		if err := rows.Scan(&inc.ID, &inc.Monitor, &started, &resolved, &inc.Reason); err != nil {
			return nil, err
		}
		inc.StartedAt = time.UnixMilli(started)
		if resolved.Valid {
			inc.ResolvedAt = time.UnixMilli(resolved.Int64)
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

// Prune deletes results older than the cutoff and resolved incidents that
// ended before it. Open incidents are always kept.
func (s *Store) Prune(olderThan time.Time) error {
	cutoff := olderThan.UnixMilli()
	if _, err := s.db.Exec(`DELETE FROM results WHERE checked_at < ?`, cutoff); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM incidents WHERE resolved_at IS NOT NULL AND resolved_at < ?`, cutoff)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Package check defines the monitor probes and the per-monitor scheduler loop.
package check

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"gjallar/internal/config"
)

// Result of a single execution of a check.
type Result struct {
	Monitor string
	Time    time.Time
	OK      bool
	Latency time.Duration
	Message string // empty on success, human-readable failure reason otherwise
}

// Checker runs one probe. Implementations must honor ctx cancellation.
type Checker interface {
	Check(ctx context.Context) (ok bool, message string)
}

// New builds a Checker from a validated monitor config.
func New(m config.Monitor) (Checker, error) {
	var (
		c   Checker
		err error
	)
	switch m.Type {
	case "http":
		c, err = newHTTPCheck(m)
	case "postgres":
		c, err = newSQLCheck("pgx", m)
	case "oracle":
		c, err = newSQLCheck("oracle", m)
	case "ping":
		c, err = newPingCheck(m)
	case "prometheus":
		c, err = newPromCheck(m)
	default:
		err = fmt.Errorf("unknown type %q", m.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("monitor %q: %w", m.Name, err)
	}
	return c, nil
}

// Run owns one monitor: an immediate first check (with a small jitter so many
// monitors don't stampede at startup), then a ticker at the configured
// interval. Each check gets its own timeout context; the Result is sent on out.
func Run(ctx context.Context, m config.Monitor, c Checker, out chan<- Result) {
	jitter := time.Duration(rand.Int63n(int64(m.Interval.D()/10) + 1))
	select {
	case <-time.After(jitter):
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(m.Interval.D())
	defer ticker.Stop()
	for {
		r := runOnce(ctx, m, c)
		select {
		case out <- r:
		case <-ctx.Done():
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func runOnce(ctx context.Context, m config.Monitor, c Checker) Result {
	cctx, cancel := context.WithTimeout(ctx, m.Timeout.D())
	defer cancel()
	start := time.Now()
	ok, msg := c.Check(cctx)
	return Result{
		Monitor: m.Name,
		Time:    start,
		OK:      ok,
		Latency: time.Since(start),
		Message: msg,
	}
}

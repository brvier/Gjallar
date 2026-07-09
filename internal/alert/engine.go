// Package alert turns check results into incidents and notifications.
package alert

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gjallar/internal/check"
	"gjallar/internal/config"
	"gjallar/internal/store"
)

// Notifier delivers one alert message on some channel.
type Notifier interface {
	Send(ctx context.Context, title, message string) error
}

const sendTimeout = 15 * time.Second

type monitorState struct {
	down         bool
	consecFails  int
	downSince    time.Time
	lastNotified time.Time
	threshold    int
	realert      time.Duration // reminder interval while down; 0 = disabled
	notifiers    []string
}

// Engine is a per-monitor up/down state machine. Process must be called from
// a single goroutine (the pipeline consumer); notifier sends are dispatched
// asynchronously so a slow channel never blocks the pipeline.
type Engine struct {
	st        *store.Store
	notifiers map[string]Notifier
	states    map[string]*monitorState
}

// NewEngine builds the engine and seeds each monitor's state from any open
// incident, so a restart neither re-fires DOWN alerts nor misses recovery.
func NewEngine(cfg *config.Config, st *store.Store, notifiers map[string]Notifier) (*Engine, error) {
	e := &Engine{st: st, notifiers: notifiers, states: map[string]*monitorState{}}
	for _, m := range cfg.Monitors {
		if !m.IsEnabled() {
			continue // disabled monitors produce no results, need no state
		}
		s := &monitorState{threshold: m.FailureThreshold, realert: m.Realert.D(), notifiers: m.Alerts}
		open, err := st.HasOpenIncident(m.Name)
		if err != nil {
			return nil, err
		}
		if open {
			s.down = true
			s.consecFails = m.FailureThreshold
			s.downSince = time.Now()
			s.lastNotified = time.Now()
		}
		e.states[m.Name] = s
	}
	return e, nil
}

func (e *Engine) Process(r check.Result) {
	s, ok := e.states[r.Monitor]
	if !ok {
		return
	}
	switch {
	case r.OK && s.down:
		s.down = false
		s.consecFails = 0
		downFor := r.Time.Sub(s.downSince).Round(time.Second)
		if err := e.st.ResolveIncident(r.Monitor, r.Time); err != nil {
			slog.Error("resolving incident", "monitor", r.Monitor, "error", err)
		}
		e.notify(s, fmt.Sprintf("[Gjallar] UP: %s", r.Monitor),
			fmt.Sprintf("%s recovered after %s", r.Monitor, downFor))
	case r.OK:
		s.consecFails = 0
	case !s.down:
		s.consecFails++
		if s.consecFails >= s.threshold {
			s.down = true
			s.downSince = r.Time
			s.lastNotified = r.Time
			if err := e.st.OpenIncident(r.Monitor, r.Message, r.Time); err != nil {
				slog.Error("opening incident", "monitor", r.Monitor, "error", err)
			}
			e.notify(s, fmt.Sprintf("[Gjallar] DOWN: %s", r.Monitor),
				fmt.Sprintf("%s — %s (%d consecutive failures)", r.Monitor, r.Message, s.consecFails))
		}
	default: // already down and still failing: remind every realert interval
		if s.realert > 0 && r.Time.Sub(s.lastNotified) >= s.realert {
			s.lastNotified = r.Time
			e.notify(s, fmt.Sprintf("[Gjallar] STILL DOWN: %s", r.Monitor),
				fmt.Sprintf("%s still down after %s — %s", r.Monitor,
					r.Time.Sub(s.downSince).Round(time.Second), r.Message))
		}
	}
}

func (e *Engine) notify(s *monitorState, title, message string) {
	slog.Info("alert", "title", title, "message", message)
	for _, name := range s.notifiers {
		n, ok := e.notifiers[name]
		if !ok {
			continue // config validation guarantees this, but stay safe
		}
		go func(name string, n Notifier) {
			ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
			defer cancel()
			if err := n.Send(ctx, title, message); err != nil {
				slog.Error("sending alert", "notifier", name, "error", err)
			}
		}(name, n)
	}
}

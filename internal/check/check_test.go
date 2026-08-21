package check

import (
	"context"
	"testing"
	"time"

	"gjallar/internal/config"
)

type fakeReporter struct {
	rtt time.Duration
}

func (f *fakeReporter) Check(ctx context.Context) (bool, string) { return true, "" }
func (f *fakeReporter) Latency() time.Duration                   { return f.rtt }

func TestRunOnceLatencyReporter(t *testing.T) {
	m := config.Monitor{Name: "t", Timeout: config.Duration(time.Second)}

	r := runOnce(context.Background(), m, &fakeReporter{rtt: 300 * time.Microsecond})
	if r.Latency != 300*time.Microsecond {
		t.Errorf("Latency = %v, want reported 300µs", r.Latency)
	}

	// A non-positive report falls back to the wall-clock duration.
	r = runOnce(context.Background(), m, &fakeReporter{rtt: 0})
	if r.Latency <= 0 {
		t.Errorf("Latency = %v, want wall-clock fallback > 0", r.Latency)
	}
}

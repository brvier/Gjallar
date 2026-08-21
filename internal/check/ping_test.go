package check

import (
	"context"
	"testing"
	"time"

	"gjallar/internal/config"
)

func TestPingLocalhost(t *testing.T) {
	if err := SelfTestPing(false); err != nil {
		t.Skipf("unprivileged ping unavailable: %v", err)
	}
	c, err := newPingCheck(config.Monitor{
		Name: "t", Type: "ping", Host: "127.0.0.1", Count: 2,
		Timeout: config.Duration(3 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, msg := c.Check(context.Background()); !ok {
		t.Errorf("ping 127.0.0.1 failed: %s", msg)
	}
	// Latency must be the ICMP RTT, not the ~200ms inter-probe interval.
	if rtt := c.Latency(); rtt <= 0 || rtt >= 200*time.Millisecond {
		t.Errorf("Latency() = %v, want a real localhost RTT", rtt)
	}
}

func TestPingUnreachable(t *testing.T) {
	if err := SelfTestPing(false); err != nil {
		t.Skipf("unprivileged ping unavailable: %v", err)
	}
	// TEST-NET-1 (RFC 5737) is reserved and never answers.
	c, err := newPingCheck(config.Monitor{
		Name: "t", Type: "ping", Host: "192.0.2.1", Count: 1,
		Timeout: config.Duration(1 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, msg := c.Check(context.Background()); ok {
		t.Errorf("expected failure, got ok (%s)", msg)
	}
	if rtt := c.Latency(); rtt != 0 {
		t.Errorf("Latency() = %v after failed check, want 0", rtt)
	}
}

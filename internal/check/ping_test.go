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
}

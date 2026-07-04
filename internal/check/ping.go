package check

import (
	"context"
	"fmt"
	"time"

	"gjallar/internal/config"

	probing "github.com/prometheus-community/pro-bing"
)

type pingCheck struct {
	host       string
	count      int
	privileged bool
	timeout    time.Duration
}

func newPingCheck(m config.Monitor) (*pingCheck, error) {
	return &pingCheck{
		host:       m.Host,
		count:      m.Count,
		privileged: m.Privileged,
		timeout:    m.Timeout.D(),
	}, nil
}

func (c *pingCheck) Check(ctx context.Context) (bool, string) {
	pinger, err := probing.NewPinger(c.host)
	if err != nil {
		return false, err.Error()
	}
	pinger.Count = c.count
	pinger.Timeout = c.timeout
	pinger.Interval = 200 * time.Millisecond
	pinger.SetPrivileged(c.privileged)

	if err := pinger.RunWithContext(ctx); err != nil {
		return false, err.Error()
	}
	stats := pinger.Statistics()
	if stats.PacketsRecv == 0 {
		return false, fmt.Sprintf("no reply from %s (%d packets sent)", c.host, stats.PacketsSent)
	}
	if stats.PacketLoss > 0 {
		return true, fmt.Sprintf("%.0f%% loss, avg %s", stats.PacketLoss, stats.AvgRtt.Round(time.Millisecond))
	}
	return true, ""
}

// SelfTestPing pings 127.0.0.1 once so a missing capability or sysctl fails
// at startup with an actionable message instead of every check failing forever.
func SelfTestPing(privileged bool) error {
	pinger, err := probing.NewPinger("127.0.0.1")
	if err != nil {
		return err
	}
	pinger.Count = 1
	pinger.Timeout = 2 * time.Second
	pinger.SetPrivileged(privileged)
	if err := pinger.Run(); err != nil {
		hint := `sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"   (unprivileged ICMP)`
		if privileged {
			hint = "sudo setcap cap_net_raw+ep ./gjallar   (or run as root / systemd AmbientCapabilities=CAP_NET_RAW)"
		}
		return fmt.Errorf("ping self-test failed: %v\nfix: %s", err, hint)
	}
	return nil
}

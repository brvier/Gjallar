package check

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"gjallar/internal/config"
)

// redisCheck connects to a Redis server, optionally authenticates, and
// expects +PONG to a PING. No client library — the protocol is two lines.
type redisCheck struct {
	addr     string
	password string
}

func newRedisCheck(m config.Monitor) (*redisCheck, error) {
	port := m.Port
	if port == 0 {
		port = 6379
	}
	return &redisCheck{
		addr:     net.JoinHostPort(m.Host, strconv.Itoa(port)),
		password: m.Password,
	}, nil
}

func (c *redisCheck) Check(ctx context.Context) (bool, string) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return false, err.Error()
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	r := bufio.NewReader(conn)

	if c.password != "" {
		if err := redisCommand(conn, r, "AUTH "+c.password, "+OK"); err != nil {
			return false, fmt.Sprintf("auth: %v", err)
		}
	}
	if err := redisCommand(conn, r, "PING", "+PONG"); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func redisCommand(conn net.Conn, r *bufio.Reader, cmd, want string) error {
	if _, err := fmt.Fprintf(conn, "%s\r\n", cmd); err != nil {
		return err
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	if line = strings.TrimSpace(line); line != want {
		return fmt.Errorf("unexpected reply %q", line)
	}
	return nil
}

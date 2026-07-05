package check

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"gjallar/internal/config"
)

// fakeRedis answers PING/AUTH like a real server; password "" = no auth needed.
func fakeRedis(t *testing.T, password string) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				authed := password == ""
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					switch cmd := strings.TrimSpace(line); {
					case strings.HasPrefix(cmd, "AUTH "):
						if strings.TrimPrefix(cmd, "AUTH ") == password {
							authed = true
							c.Write([]byte("+OK\r\n"))
						} else {
							c.Write([]byte("-ERR invalid password\r\n"))
						}
					case cmd == "PING" && authed:
						c.Write([]byte("+PONG\r\n"))
					default:
						c.Write([]byte("-NOAUTH Authentication required.\r\n"))
					}
				}
			}(conn)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func redisMonitor(host string, port int, password string) config.Monitor {
	return config.Monitor{
		Name: "t", Type: "redis", Host: host, Port: port, Password: password,
		Timeout: config.Duration(3 * time.Second),
	}
}

func TestRedisCheck(t *testing.T) {
	host, port := fakeRedis(t, "")
	c, _ := newRedisCheck(redisMonitor(host, port, ""))
	if ok, msg := c.Check(context.Background()); !ok {
		t.Errorf("expected ok, got %q", msg)
	}
}

func TestRedisCheckAuth(t *testing.T) {
	host, port := fakeRedis(t, "s3cret")
	c, _ := newRedisCheck(redisMonitor(host, port, "s3cret"))
	if ok, msg := c.Check(context.Background()); !ok {
		t.Errorf("expected ok, got %q", msg)
	}

	bad, _ := newRedisCheck(redisMonitor(host, port, "wrong"))
	if ok, msg := bad.Check(context.Background()); ok || !strings.Contains(msg, "auth") {
		t.Errorf("got ok=%v msg=%q", ok, msg)
	}

	noauth, _ := newRedisCheck(redisMonitor(host, port, ""))
	if ok, _ := noauth.Check(context.Background()); ok {
		t.Error("expected NOAUTH failure")
	}
}

func TestRedisCheckDown(t *testing.T) {
	c, _ := newRedisCheck(redisMonitor("127.0.0.1", 1, ""))
	if ok, _ := c.Check(context.Background()); ok {
		t.Error("expected connection failure")
	}
}

func TestRedisDefaultPort(t *testing.T) {
	c, _ := newRedisCheck(config.Monitor{Host: "h"})
	if c.addr != "h:"+strconv.Itoa(6379) {
		t.Errorf("addr = %q", c.addr)
	}
}

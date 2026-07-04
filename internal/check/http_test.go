package check

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gjallar/internal/config"
)

func httpMonitor(url string) config.Monitor {
	return config.Monitor{
		Name: "t", Type: "http", URL: url, Method: "GET",
		Timeout: config.Duration(5 * time.Second),
	}
}

func TestHTTPCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte(`{"status": "ok"}`))
		case "/teapot":
			w.WriteHeader(http.StatusTeapot)
		case "/auth":
			if r.Header.Get("Authorization") != "Bearer xyz" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte("welcome"))
		}
	}))
	defer srv.Close()

	t.Run("2xx default", func(t *testing.T) {
		c, _ := newHTTPCheck(httpMonitor(srv.URL + "/ok"))
		if ok, msg := c.Check(context.Background()); !ok {
			t.Errorf("expected ok, got %q", msg)
		}
	})
	t.Run("non-2xx fails by default", func(t *testing.T) {
		c, _ := newHTTPCheck(httpMonitor(srv.URL + "/teapot"))
		ok, msg := c.Check(context.Background())
		if ok || !strings.Contains(msg, "418") {
			t.Errorf("got ok=%v msg=%q", ok, msg)
		}
	})
	t.Run("expect_status matches exact code", func(t *testing.T) {
		m := httpMonitor(srv.URL + "/teapot")
		m.ExpectStatus = 418
		c, _ := newHTTPCheck(m)
		if ok, msg := c.Check(context.Background()); !ok {
			t.Errorf("expected ok, got %q", msg)
		}
	})
	t.Run("expect_status mismatch", func(t *testing.T) {
		m := httpMonitor(srv.URL + "/ok")
		m.ExpectStatus = 201
		c, _ := newHTTPCheck(m)
		ok, msg := c.Check(context.Background())
		if ok || !strings.Contains(msg, "expected 201") {
			t.Errorf("got ok=%v msg=%q", ok, msg)
		}
	})
	t.Run("body regex match", func(t *testing.T) {
		m := httpMonitor(srv.URL + "/ok")
		m.BodyRegex = `"status"\s*:\s*"ok"`
		c, _ := newHTTPCheck(m)
		if ok, msg := c.Check(context.Background()); !ok {
			t.Errorf("expected ok, got %q", msg)
		}
	})
	t.Run("body regex miss", func(t *testing.T) {
		m := httpMonitor(srv.URL + "/ok")
		m.BodyRegex = `"status": "down"`
		c, _ := newHTTPCheck(m)
		ok, msg := c.Check(context.Background())
		if ok || !strings.Contains(msg, "does not match") {
			t.Errorf("got ok=%v msg=%q", ok, msg)
		}
	})
	t.Run("headers sent", func(t *testing.T) {
		m := httpMonitor(srv.URL + "/auth")
		m.Headers = map[string]string{"Authorization": "Bearer xyz"}
		c, _ := newHTTPCheck(m)
		if ok, msg := c.Check(context.Background()); !ok {
			t.Errorf("expected ok, got %q", msg)
		}
	})
	t.Run("connection refused", func(t *testing.T) {
		c, _ := newHTTPCheck(httpMonitor("http://127.0.0.1:1/nope"))
		if ok, _ := c.Check(context.Background()); ok {
			t.Error("expected failure")
		}
	})
}

func TestHTTPCheckTLSExpiry(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	newTLSCheck := func(warn time.Duration) *httpCheck {
		m := httpMonitor(srv.URL)
		m.CertExpiryWarn = config.Duration(warn)
		c, err := newHTTPCheck(m)
		if err != nil {
			t.Fatal(err)
		}
		c.client = &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: nil, InsecureSkipVerify: true},
		}}
		return c
	}

	// httptest certs are valid ~10 years: a 1h window passes, a 100y window fails.
	t.Run("not expiring soon", func(t *testing.T) {
		c := newTLSCheck(time.Hour)
		if ok, msg := c.Check(context.Background()); !ok {
			t.Errorf("expected ok, got %q", msg)
		}
	})
	t.Run("expiring within window", func(t *testing.T) {
		c := newTLSCheck(100 * 365 * 24 * time.Hour)
		ok, msg := c.Check(context.Background())
		if ok || !strings.Contains(msg, "TLS certificate expires") {
			t.Errorf("got ok=%v msg=%q", ok, msg)
		}
	})
	t.Run("disabled ignores expiry", func(t *testing.T) {
		c := newTLSCheck(0)
		if ok, msg := c.Check(context.Background()); !ok {
			t.Errorf("expected ok, got %q", msg)
		}
	})
}

func TestRunOnceTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	m := httpMonitor(srv.URL)
	m.Timeout = config.Duration(50 * time.Millisecond)
	c, _ := newHTTPCheck(m)
	r := runOnce(context.Background(), m, c)
	if r.OK {
		t.Error("expected timeout failure")
	}
	if r.Latency > time.Second {
		t.Errorf("latency %v suggests timeout was not honored", r.Latency)
	}
}

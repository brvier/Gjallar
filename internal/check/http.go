package check

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"gjallar/internal/config"
)

const maxBodyBytes = 1 << 20 // regex matching is capped at 1 MiB of body

type httpCheck struct {
	url            string
	method         string
	headers        map[string]string
	expectStatus   int // 0 = any 2xx
	bodyRe         *regexp.Regexp
	certExpiryWarn time.Duration
	client         *http.Client
}

func newHTTPCheck(m config.Monitor) (*httpCheck, error) {
	c := &httpCheck{
		url:            m.URL,
		method:         m.Method,
		headers:        m.Headers,
		expectStatus:   m.ExpectStatus,
		certExpiryWarn: m.CertExpiryWarn.D(),
		client:         &http.Client{},
	}
	if m.BodyRegex != "" {
		re, err := regexp.Compile(m.BodyRegex)
		if err != nil {
			return nil, fmt.Errorf("body_regex: %w", err)
		}
		c.bodyRe = re
	}
	return c, nil
}

func (c *httpCheck) Check(ctx context.Context) (bool, string) {
	req, err := http.NewRequestWithContext(ctx, c.method, c.url, nil)
	if err != nil {
		return false, err.Error()
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()

	if c.expectStatus != 0 {
		if resp.StatusCode != c.expectStatus {
			return false, fmt.Sprintf("status %d, expected %d", resp.StatusCode, c.expectStatus)
		}
	} else if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return false, fmt.Sprintf("status %d, expected 2xx", resp.StatusCode)
	}

	if c.bodyRe != nil {
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		if err != nil {
			return false, fmt.Sprintf("reading body: %v", err)
		}
		if !c.bodyRe.Match(body) {
			return false, fmt.Sprintf("body does not match %q", c.bodyRe.String())
		}
	}

	// Chain validity is already enforced by the TLS handshake; here we only
	// warn ahead of expiry.
	if c.certExpiryWarn > 0 && resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		notAfter := resp.TLS.PeerCertificates[0].NotAfter
		if left := time.Until(notAfter); left < c.certExpiryWarn {
			return false, fmt.Sprintf("TLS certificate expires in %s (%s)",
				formatDays(left), notAfter.Format("2006-01-02"))
		}
	}
	return true, ""
}

func formatDays(d time.Duration) string {
	if d < 0 {
		return "the past — already expired"
	}
	if d >= 48*time.Hour {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return d.Round(time.Minute).String()
}

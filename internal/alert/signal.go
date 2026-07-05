package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Signal sends messages through a signal-cli-rest-api compatible gateway
// (POST {url} with {"message", "number", "recipients"}).
type Signal struct {
	URL        string // full /v2/send endpoint
	Number     string // sender number registered on the gateway
	Recipients []string
	Client     *http.Client
}

func NewSignal(url, number string, recipients []string) *Signal {
	return &Signal{URL: url, Number: number, Recipients: recipients, Client: &http.Client{}}
}

func (s *Signal) Send(ctx context.Context, title, message string) error {
	payload, err := json.Marshal(map[string]any{
		"message":    title + "\n" + message,
		"number":     s.Number,
		"recipients": s.Recipients,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("signal: status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return nil
}

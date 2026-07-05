package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gjallar/internal/config"
)

func TestSignal(t *testing.T) {
	var got map[string]any
	status := 201
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("method=%s content-type=%s", r.Method, r.Header.Get("Content-Type"))
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
	}))
	defer srv.Close()

	s := NewSignal(srv.URL, "+33616400522", []string{"+33689816957", "+33616400522"})
	if err := s.Send(context.Background(), "[Gjallar] DOWN: web", "web — boom"); err != nil {
		t.Fatal(err)
	}
	if got["number"] != "+33616400522" {
		t.Errorf("number = %v", got["number"])
	}
	if recs, _ := got["recipients"].([]any); len(recs) != 2 || recs[0] != "+33689816957" {
		t.Errorf("recipients = %v", got["recipients"])
	}
	if msg, _ := got["message"].(string); !strings.Contains(msg, "DOWN: web") || !strings.Contains(msg, "boom") {
		t.Errorf("message = %q", got["message"])
	}

	status = 400
	if err := s.Send(context.Background(), "t", "m"); err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Errorf("err = %v", err)
	}
}

func TestBuildNotifiersSignal(t *testing.T) {
	ns, err := BuildNotifiers(map[string]config.Alert{
		"sig": {Type: "signal", URL: "http://h/v2/send", Number: "+336", Recipients: []string{"+337"}},
	})
	if err != nil || ns["sig"] == nil {
		t.Fatalf("BuildNotifiers: %v", err)
	}
}

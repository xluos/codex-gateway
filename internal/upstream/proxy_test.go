package upstream

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProxy_StreamsSSEWithoutChangingStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: hello\n\n"))
	}))
	defer upstream.Close()

	client := NewClient(upstream.URL, "sk-test", time.Minute)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4.1","stream":true}`))

	_, err := Proxy(rec, req, client, "/v1/chat/completions")
	if err != nil {
		t.Fatalf("Proxy returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("unexpected content type: %q", got)
	}
	if body := rec.Body.String(); body != "data: hello\n\n" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestProxy_PreservesUpstreamErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer upstream.Close()

	client := NewClient(upstream.URL, "sk-test", time.Minute)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)

	_, err := Proxy(rec, req, client, "/v1/models")
	if err != nil {
		t.Fatalf("Proxy returned error: %v", err)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != `{"error":{"message":"rate limited"}}` {
		t.Fatalf("unexpected body: %q", body)
	}
}

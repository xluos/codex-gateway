package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codex-gateway/internal/config"
)

func TestProbeAccounts_UpdatesMissingSnapshots(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["model"] != "gpt-4.1-mini" {
			t.Fatalf("unexpected model: %#v", body["model"])
		}
		w.Header().Set("X-Codex-5h-Used-Percent", "75")
		w.Header().Set("X-Codex-5h-Reset-After-Seconds", "3600")
		w.Header().Set("X-Codex-7d-Used-Percent", "25")
		w.Header().Set("X-Codex-7d-Reset-After-Seconds", "7200")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1"}`))
	}))
	defer server.Close()

	pool := NewOpenAIAccountPool([]config.NamedUpstreamConfig{
		{Name: "primary", Mode: "api_key", BaseURL: server.URL, APIKey: "sk-primary", Priority: 10, DefaultModel: "gpt-4.1-mini"},
	}, nil, time.Minute)

	if err := pool.ProbeAccounts(context.Background(), []string{"primary"}); err != nil {
		t.Fatalf("ProbeAccounts returned error: %v", err)
	}

	statuses := pool.AccountsStatus()
	if statuses[0].Codex5hUsedPercent == nil || *statuses[0].Codex5hUsedPercent != 75 {
		t.Fatalf("unexpected 5h usage: %#v", statuses[0].Codex5hUsedPercent)
	}
	if statuses[0].Codex7dUsedPercent == nil || *statuses[0].Codex7dUsedPercent != 25 {
		t.Fatalf("unexpected 7d usage: %#v", statuses[0].Codex7dUsedPercent)
	}
}

func TestProbeAccounts_SetsLastErrorOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"bad gateway"}`))
	}))
	defer server.Close()

	pool := NewOpenAIAccountPool([]config.NamedUpstreamConfig{
		{Name: "primary", Mode: "api_key", BaseURL: server.URL, APIKey: "sk-primary", Priority: 10, DefaultModel: "gpt-4.1-mini"},
	}, nil, time.Minute)

	err := pool.ProbeAccounts(context.Background(), []string{"primary"})
	if err == nil {
		t.Fatal("expected ProbeAccounts to fail")
	}
	if !strings.Contains(err.Error(), "primary") {
		t.Fatalf("unexpected probe error: %v", err)
	}

	statuses := pool.AccountsStatus()
	if statuses[0].LastError == "" {
		t.Fatal("expected last error to be recorded")
	}
}

func TestProbeAccounts_UsesNormalizedOAuthCodexRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/codex/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["instructions"] == nil {
			t.Fatalf("expected instructions in oauth probe body: %#v", body)
		}
		if body["store"] != false {
			t.Fatalf("expected store=false in oauth probe body: %#v", body)
		}
		if body["stream"] != true {
			t.Fatalf("expected stream=true in oauth probe body: %#v", body)
		}
		w.Header().Set("X-Codex-5h-Used-Percent", "65")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"rate limited"}`))
	}))
	defer server.Close()

	pool := NewOpenAIAccountPool([]config.NamedUpstreamConfig{
		{
			Name:         "default",
			Mode:         "oauth",
			BaseURL:      server.URL,
			DefaultModel: "gpt-5.1-codex-mini",
			OAuth: config.UpstreamOAuthConfig{
				CredentialsFile: "",
			},
		},
	}, map[string]AccessTokenSource{
		"default": staticTokenSource{token: "token"},
	}, time.Minute)

	if err := pool.ProbeAccounts(context.Background(), []string{"default"}); err != nil {
		t.Fatalf("ProbeAccounts returned error: %v", err)
	}

	statuses := pool.AccountsStatus()
	if statuses[0].Codex5hUsedPercent == nil || *statuses[0].Codex5hUsedPercent != 65 {
		t.Fatalf("unexpected 5h usage: %#v", statuses[0].Codex5hUsedPercent)
	}
}

func TestProbeAccounts_AcceptsPrimarySecondaryHeadersOnRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Codex-Primary-Used-Percent", "20")
		w.Header().Set("X-Codex-Primary-Reset-After-Seconds", "604800")
		w.Header().Set("X-Codex-Primary-Window-Minutes", "10080")
		w.Header().Set("X-Codex-Secondary-Used-Percent", "80")
		w.Header().Set("X-Codex-Secondary-Reset-After-Seconds", "3600")
		w.Header().Set("X-Codex-Secondary-Window-Minutes", "300")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"rate limited"}`))
	}))
	defer server.Close()

	pool := NewOpenAIAccountPool([]config.NamedUpstreamConfig{
		{Name: "primary", Mode: "api_key", BaseURL: server.URL, APIKey: "sk-primary", Priority: 10, DefaultModel: "gpt-4.1-mini"},
	}, nil, time.Minute)

	if err := pool.ProbeAccounts(context.Background(), []string{"primary"}); err != nil {
		t.Fatalf("ProbeAccounts returned error: %v", err)
	}

	statuses := pool.AccountsStatus()
	if statuses[0].Codex5hUsedPercent == nil || *statuses[0].Codex5hUsedPercent != 80 {
		t.Fatalf("unexpected normalized 5h usage: %#v", statuses[0].Codex5hUsedPercent)
	}
	if statuses[0].Codex7dUsedPercent == nil || *statuses[0].Codex7dUsedPercent != 20 {
		t.Fatalf("unexpected normalized 7d usage: %#v", statuses[0].Codex7dUsedPercent)
	}
}

type staticTokenSource struct {
	token string
}

func (s staticTokenSource) AccessToken(context.Context) (string, error) {
	return s.token, nil
}

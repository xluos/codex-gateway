package upstream

import (
	"net/http"
	"testing"
	"time"

	"codex-gateway/internal/config"
)

func TestAccountPool_SelectPrefersExplicitModelMapping(t *testing.T) {
	pool := NewOpenAIAccountPool([]config.NamedUpstreamConfig{
		{
			Name:         "general",
			Mode:         "api_key",
			BaseURL:      "https://api.openai.com",
			APIKey:       "sk-general",
			Priority:     20,
			DefaultModel: "gpt-4.1",
		},
		{
			Name:         "mapped",
			Mode:         "api_key",
			BaseURL:      "https://api.openai.com",
			APIKey:       "sk-mapped",
			Priority:     10,
			DefaultModel: "gpt-4.1",
			ModelMapping: map[string]string{"gpt-4.1": "gpt-4.1-mini"},
		},
	}, nil, time.Minute)

	account, resolvedModel, err := pool.Select("gpt-4.1", nil)
	if err != nil {
		t.Fatalf("Select returned error: %v", err)
	}
	if account.Name != "mapped" {
		t.Fatalf("expected mapped account, got %q", account.Name)
	}
	if resolvedModel != "gpt-4.1-mini" {
		t.Fatalf("unexpected resolved model: %q", resolvedModel)
	}
}

func TestAccountPool_SelectUsesDefaultModelWhenRequestOmitsModel(t *testing.T) {
	pool := NewOpenAIAccountPool([]config.NamedUpstreamConfig{
		{
			Name:         "primary",
			Mode:         "api_key",
			BaseURL:      "https://api.openai.com",
			APIKey:       "sk-primary",
			Priority:     10,
			DefaultModel: "gpt-4.1",
		},
	}, nil, time.Minute)

	account, resolvedModel, err := pool.Select("", nil)
	if err != nil {
		t.Fatalf("Select returned error: %v", err)
	}
	if account.Name != "primary" {
		t.Fatalf("unexpected account: %q", account.Name)
	}
	if resolvedModel != "gpt-4.1" {
		t.Fatalf("unexpected resolved model: %q", resolvedModel)
	}
}

func TestAccountPool_SelectRoundRobinsSamePriorityAccounts(t *testing.T) {
	pool := NewOpenAIAccountPool([]config.NamedUpstreamConfig{
		{Name: "a", Mode: "api_key", BaseURL: "https://api.openai.com", APIKey: "sk-a", Priority: 10, DefaultModel: "gpt-4.1"},
		{Name: "b", Mode: "api_key", BaseURL: "https://api.openai.com", APIKey: "sk-b", Priority: 10, DefaultModel: "gpt-4.1"},
	}, nil, time.Minute)

	first, _, err := pool.Select("gpt-4.1", nil)
	if err != nil {
		t.Fatalf("first Select returned error: %v", err)
	}
	second, _, err := pool.Select("gpt-4.1", nil)
	if err != nil {
		t.Fatalf("second Select returned error: %v", err)
	}
	if first.Name == second.Name {
		t.Fatalf("expected round robin to select different accounts, got %q twice", first.Name)
	}
}

func TestAccountPool_SelectSkipsCooldownAccount(t *testing.T) {
	pool := NewOpenAIAccountPool([]config.NamedUpstreamConfig{
		{Name: "primary", Mode: "api_key", BaseURL: "https://api.openai.com", APIKey: "sk-primary", Priority: 10, DefaultModel: "gpt-4.1", CooldownSeconds: 60},
		{Name: "backup", Mode: "api_key", BaseURL: "https://api.openai.com", APIKey: "sk-backup", Priority: 20, DefaultModel: "gpt-4.1"},
	}, nil, time.Minute)

	pool.MarkCooldown("primary", time.Now().Add(time.Minute), "quota")

	account, _, err := pool.Select("gpt-4.1", nil)
	if err != nil {
		t.Fatalf("Select returned error: %v", err)
	}
	if account.Name != "backup" {
		t.Fatalf("expected backup account, got %q", account.Name)
	}
}

func TestAccountPool_StatusReflectsSnapshotAndCooldown(t *testing.T) {
	pool := NewOpenAIAccountPool([]config.NamedUpstreamConfig{
		{Name: "primary", Mode: "api_key", BaseURL: "https://api.openai.com", APIKey: "sk-primary", Priority: 10, DefaultModel: "gpt-4.1", CooldownSeconds: 60},
	}, nil, time.Minute)

	resetAt := time.Now().Add(2 * time.Hour).UTC()
	pool.UpdateSnapshot("primary", http.Header{
		"X-Codex-5h-Used-Percent":         []string{"75"},
		"X-Codex-5h-Reset-After-Seconds":  []string{"3600"},
		"X-Codex-7d-Used-Percent":         []string{"25"},
		"X-Codex-7d-Reset-After-Seconds":  []string{"7200"},
	})
	pool.MarkCooldown("primary", resetAt, "rate_limit")

	statuses := pool.AccountsStatus()
	if len(statuses) != 1 {
		t.Fatalf("unexpected status count: %d", len(statuses))
	}
	if statuses[0].Status != "cooldown" {
		t.Fatalf("unexpected status: %q", statuses[0].Status)
	}
	if statuses[0].Codex5hUsedPercent == nil || *statuses[0].Codex5hUsedPercent != 75 {
		t.Fatalf("unexpected 5h usage: %#v", statuses[0].Codex5hUsedPercent)
	}
	if statuses[0].CooldownUntil == nil {
		t.Fatal("expected cooldown time")
	}
}

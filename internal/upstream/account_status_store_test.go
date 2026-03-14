package upstream

import (
	"path/filepath"
	"testing"
	"time"

	"codex-gateway/internal/config"
)

func TestAccountStatusStore_SaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts-status.json")
	now := time.Now().UTC().Truncate(time.Second)
	statuses := []AccountStatus{
		{
			Name:               "primary",
			Mode:               "api_key",
			Priority:           10,
			DefaultModel:       "gpt-4.1",
			Status:             "available",
			SnapshotUpdatedAt:  &now,
			Codex5hUsedPercent: float64PtrForStoreTest(75),
			Codex7dUsedPercent: float64PtrForStoreTest(25),
		},
	}

	if err := SaveAccountStatuses(path, statuses); err != nil {
		t.Fatalf("SaveAccountStatuses returned error: %v", err)
	}

	loaded, err := LoadAccountStatuses(path)
	if err != nil {
		t.Fatalf("LoadAccountStatuses returned error: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("unexpected status count: %d", len(loaded))
	}
	if loaded[0].Name != "primary" {
		t.Fatalf("unexpected status name: %q", loaded[0].Name)
	}
	if loaded[0].Codex5hUsedPercent == nil || *loaded[0].Codex5hUsedPercent != 75 {
		t.Fatalf("unexpected 5h usage: %#v", loaded[0].Codex5hUsedPercent)
	}
}

func TestOpenAIAccountPool_ApplyPersistedStatuses(t *testing.T) {
	pool := NewOpenAIAccountPool([]config.NamedUpstreamConfig{
		{Name: "primary", Mode: "api_key", BaseURL: "https://api.openai.com", APIKey: "sk-primary", Priority: 10, DefaultModel: "gpt-4.1"},
		{Name: "backup", Mode: "api_key", BaseURL: "https://api.openai.com", APIKey: "sk-backup", Priority: 20, DefaultModel: "gpt-4.1-mini"},
	}, nil, time.Minute)

	resetAt := time.Now().Add(30 * time.Minute).UTC()
	pool.ApplyPersistedStatuses([]AccountStatus{
		{
			Name:               "primary",
			Status:             "available",
			Codex5hUsedPercent: float64PtrForStoreTest(80),
			Codex5hResetAt:     &resetAt,
			LastError:          "quota probe",
		},
	})

	statuses := pool.AccountsStatus()
	if len(statuses) != 2 {
		t.Fatalf("unexpected status count: %d", len(statuses))
	}
	if statuses[0].Name != "primary" {
		t.Fatalf("unexpected first status: %#v", statuses[0])
	}
	if statuses[0].Codex5hUsedPercent == nil || *statuses[0].Codex5hUsedPercent != 80 {
		t.Fatalf("unexpected applied 5h usage: %#v", statuses[0].Codex5hUsedPercent)
	}
	if statuses[0].LastError != "quota probe" {
		t.Fatalf("unexpected last error: %q", statuses[0].LastError)
	}
	if statuses[1].Codex5hUsedPercent != nil {
		t.Fatalf("expected backup account to remain untouched, got %#v", statuses[1].Codex5hUsedPercent)
	}
}

func float64PtrForStoreTest(v float64) *float64 {
	return &v
}

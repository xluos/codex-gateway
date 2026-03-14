package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-gateway/internal/config"
	"codex-gateway/internal/upstream"
)

func TestRunHelp_PrintsCLIOverview(t *testing.T) {
	var out bytes.Buffer

	if err := run([]string{"help"}, &out); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	text := out.String()
	for _, want := range []string{
		"Codex Gateway",
		"init",
		"doctor",
		"auth login",
		"start",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected help output to contain %q, got %q", want, text)
		}
	}
}

func TestRunHelp_IncludesCompletionCommand(t *testing.T) {
	var out bytes.Buffer

	err := run([]string{"help"}, &out)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	text := out.String()
	for _, want := range []string{
		"completion",
		"completion zsh|bash|fish",
		"shell 补全脚本",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected help output to contain %q, got %q", want, text)
		}
	}
}

func TestRunAccountsStatus(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configBody := `
auth:
  api_keys:
    - local-key
upstreams:
  - name: primary
    mode: api_key
    base_url: https://api.openai.com
    api_key: sk-primary
    priority: 10
    default_model: gpt-4.1
  - name: backup
    mode: api_key
    base_url: https://api.openai.com
    api_key: sk-backup
    priority: 20
    default_model: gpt-4.1-mini
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	statuses := []upstream.AccountStatus{
		{
			Name:               "primary",
			Mode:               "api_key",
			Priority:           10,
			DefaultModel:       "gpt-4.1",
			Status:             "available",
			Codex5hUsedPercent: float64Ptr(75),
			Codex5hResetAt:     timePtr("2026-03-13T11:22:00Z"),
			Codex7dUsedPercent: float64Ptr(25),
			Codex7dResetAt:     timePtr("2026-03-15T10:22:00Z"),
		},
		{
			Name:         "backup",
			Mode:         "api_key",
			Priority:     20,
			DefaultModel: "gpt-4.1-mini",
			Status:       "cooldown",
		},
	}
	restore := setAccountsStatusStreamerForTest(func(_ *config.Config, w io.Writer) error {
		for _, item := range statuses {
			printAccountStatus(w, item)
		}
		return nil
	})
	defer restore()

	var out bytes.Buffer
	err := run([]string{"--config", configPath, "accounts", "status"}, &out)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	text := out.String()
	for _, want := range []string{
		"primary",
		"backup",
		"available",
		"gpt-4.1",
		"gpt-4.1-mini",
		"███████████████",
		"75.0%",
		"25.0%",
		"reset in",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected accounts status output to contain %q, got %q", want, text)
		}
	}
}

func TestRunAccountsStatusJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configBody := `
auth:
  api_keys:
    - local-key
upstreams:
  - name: primary
    mode: api_key
    base_url: https://api.openai.com
    api_key: sk-primary
    priority: 10
    default_model: gpt-4.1
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	statuses := []upstream.AccountStatus{
		{
			Name:               "primary",
			Mode:               "api_key",
			Priority:           10,
			DefaultModel:       "gpt-4.1",
			Status:             "available",
			Codex5hUsedPercent: float64Ptr(75),
			Codex7dUsedPercent: float64Ptr(25),
		},
	}
	restore := setAccountsStatusProviderForTest(func(*config.Config) ([]upstream.AccountStatus, error) {
		return statuses, nil
	})
	defer restore()

	var out bytes.Buffer
	err := run([]string{"--config", configPath, "accounts", "status", "--json"}, &out)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	var decoded []map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if len(decoded) != 1 || decoded[0]["name"] != "primary" {
		t.Fatalf("unexpected json payload: %s", out.String())
	}
}

func TestResolveAccountsStatus_UsesPersistedSnapshots(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Runtime: config.RuntimeConfig{Dir: tmpDir},
		Upstreams: []config.NamedUpstreamConfig{
			{Name: "primary", Mode: "api_key", BaseURL: "https://api.openai.com", APIKey: "sk-primary", Priority: 10, DefaultModel: "gpt-4.1"},
		},
	}

	restorePool := setAccountPoolFactoryForTest(func(*config.Config) (*upstream.OpenAIAccountPool, error) {
		return upstream.NewOpenAIAccountPool(cfg.Upstreams, nil, time.Minute), nil
	})
	defer restorePool()

	restoreLoader := setAccountStatusLoaderForTest(func(string) ([]upstream.AccountStatus, error) {
		return []upstream.AccountStatus{{
			Name:               "primary",
			Codex5hUsedPercent: float64Ptr(80),
			Codex7dUsedPercent: float64Ptr(30),
		}}, nil
	})
	defer restoreLoader()

	probed := false
	restoreProber := setAccountStatusProberForTest(func(pool *upstream.OpenAIAccountPool, _ []upstream.AccountStatus) error {
		probed = true
		return nil
	})
	defer restoreProber()

	statuses, err := loadAccountsStatus(cfg)
	if err != nil {
		t.Fatalf("loadAccountsStatus returned error: %v", err)
	}
	if probed {
		t.Fatal("expected persisted snapshots to skip probing")
	}
	if len(statuses) != 1 || statuses[0].Codex5hUsedPercent == nil || *statuses[0].Codex5hUsedPercent != 80 {
		t.Fatalf("unexpected statuses: %#v", statuses)
	}
}

func TestResolveAccountsStatus_ProbesWhenSnapshotMissing(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Runtime: config.RuntimeConfig{Dir: tmpDir},
		Upstreams: []config.NamedUpstreamConfig{
			{Name: "primary", Mode: "api_key", BaseURL: "https://api.openai.com", APIKey: "sk-primary", Priority: 10, DefaultModel: "gpt-4.1"},
		},
	}

	restorePool := setAccountPoolFactoryForTest(func(*config.Config) (*upstream.OpenAIAccountPool, error) {
		return upstream.NewOpenAIAccountPool(cfg.Upstreams, nil, time.Minute), nil
	})
	defer restorePool()

	restoreLoader := setAccountStatusLoaderForTest(func(string) ([]upstream.AccountStatus, error) {
		return nil, os.ErrNotExist
	})
	defer restoreLoader()

	restoreProber := setAccountStatusProberForTest(func(pool *upstream.OpenAIAccountPool, _ []upstream.AccountStatus) error {
		pool.ApplyPersistedStatuses([]upstream.AccountStatus{{
			Name:               "primary",
			Codex5hUsedPercent: float64Ptr(60),
		}})
		return nil
	})
	defer restoreProber()

	statuses, err := loadAccountsStatus(cfg)
	if err != nil {
		t.Fatalf("loadAccountsStatus returned error: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Codex5hUsedPercent == nil || *statuses[0].Codex5hUsedPercent != 60 {
		t.Fatalf("unexpected statuses: %#v", statuses)
	}
}

func TestResolveAccountsStatus_PreservesProbeErrorState(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Runtime: config.RuntimeConfig{Dir: tmpDir},
		Upstreams: []config.NamedUpstreamConfig{
			{Name: "default", Mode: "oauth", BaseURL: "https://api.openai.com", Priority: 0},
		},
	}

	restorePool := setAccountPoolFactoryForTest(func(*config.Config) (*upstream.OpenAIAccountPool, error) {
		return upstream.NewOpenAIAccountPool(cfg.Upstreams, nil, time.Minute), nil
	})
	defer restorePool()

	restoreLoader := setAccountStatusLoaderForTest(func(string) ([]upstream.AccountStatus, error) {
		return nil, os.ErrNotExist
	})
	defer restoreLoader()

	restoreProber := setAccountStatusProberForTest(func(pool *upstream.OpenAIAccountPool, _ []upstream.AccountStatus) error {
		pool.ApplyPersistedStatuses([]upstream.AccountStatus{{
			Name:      "default",
			LastError: "probe failed",
		}})
		return os.ErrPermission
	})
	defer restoreProber()

	statuses, err := loadAccountsStatus(cfg)
	if err != nil {
		t.Fatalf("loadAccountsStatus returned error: %v", err)
	}
	if len(statuses) != 1 || statuses[0].LastError != "probe failed" {
		t.Fatalf("expected probe error state to be preserved, got %#v", statuses)
	}
}

func TestAuthLoginCommand_PassesAccountFlag(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configBody := `
auth:
  api_keys:
    - local-key
upstream:
  mode: oauth
  base_url: https://api.openai.com
oauth:
  credentials_file: ` + filepath.Join(tmpDir, "oauth.json") + `
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var gotAccount string
	restore := setAuthLoginRunnerForTest(func(_ context.Context, _ *config.Config, _ io.Reader, _ io.Writer, account string) error {
		gotAccount = account
		return nil
	})
	defer restore()

	var out bytes.Buffer
	err := run([]string{"--config", configPath, "auth", "login", "--account", "backup"}, &out)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if gotAccount != "backup" {
		t.Fatalf("expected account flag to be passed through, got %q", gotAccount)
	}
}

func TestNewUpstreamAccountPool_PasswordOAuth(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		OAuth: config.OAuthConfig{
			CallbackHost: "localhost",
			CallbackPort: 1455,
			CallbackPath: "/auth/callback",
		},
		Upstreams: []config.NamedUpstreamConfig{
			{
				Name:     "password-account",
				Mode:     "password_oauth",
				BaseURL:  "https://api.openai.com",
				Email:    "user@example.com",
				Password: "secret",
				OAuth: config.UpstreamOAuthConfig{
					CredentialsFile: filepath.Join(tmpDir, "password-oauth.json"),
				},
			},
		},
	}

	pool, err := newUpstreamAccountPool(cfg)
	if err != nil {
		t.Fatalf("newUpstreamAccountPool returned error: %v", err)
	}
	if pool == nil {
		t.Fatal("expected pool")
	}
}

func float64Ptr(v float64) *float64 {
	return &v
}

func timePtr(raw string) *time.Time {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		panic(err)
	}
	return &t
}

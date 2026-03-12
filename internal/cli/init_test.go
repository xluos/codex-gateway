package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codex-gateway/internal/config"
)

func TestInit_RefusesToOverwriteExistingConfigWithoutForce(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("existing: true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var out bytes.Buffer
	err := Init(configPath, false, strings.NewReader(""), &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInit_WritesAPIKeyConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	input := strings.NewReader(strings.Join([]string{
		"local-test-key",
		"1",
		"sk-upstream-test",
		"",
	}, "\n"))
	var out bytes.Buffer

	if err := Init(configPath, false, input, &out); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	file, err := os.Open(configPath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer file.Close()

	cfg, err := config.LoadConfig(file)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Auth.APIKeys[0] != "local-test-key" {
		t.Fatalf("unexpected local key: %#v", cfg.Auth.APIKeys)
	}
	if cfg.Upstream.Mode != "api_key" {
		t.Fatalf("unexpected mode: %q", cfg.Upstream.Mode)
	}
	if cfg.Upstream.APIKey != "sk-upstream-test" {
		t.Fatalf("unexpected upstream key: %q", cfg.Upstream.APIKey)
	}
	if !strings.Contains(out.String(), "codexgateway serve") {
		t.Fatalf("expected usage hint in output: %q", out.String())
	}
}

func TestInit_WritesOAuthConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	input := strings.NewReader(strings.Join([]string{
		"local-test-key",
		"2",
		"",
	}, "\n"))
	var out bytes.Buffer

	if err := Init(configPath, false, input, &out); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	file, err := os.Open(configPath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer file.Close()

	cfg, err := config.LoadConfig(file)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Upstream.Mode != "oauth" {
		t.Fatalf("unexpected mode: %q", cfg.Upstream.Mode)
	}
	if cfg.Upstream.APIKey != "" {
		t.Fatalf("expected empty upstream api key, got %q", cfg.Upstream.APIKey)
	}
	if !strings.Contains(out.String(), "codexgateway auth login") {
		t.Fatalf("expected oauth next-step hint in output: %q", out.String())
	}
}

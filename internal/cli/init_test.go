package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codex-gateway/internal/config"
)

func TestInit_ShowsFriendlyMessageWhenConfigAlreadyExists(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("existing: true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var out bytes.Buffer
	err := Init(configPath, false, strings.NewReader(""), &out)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "已检测到现有配置文件") {
		t.Fatalf("expected friendly existing-config message, got: %q", output)
	}
	if !strings.Contains(output, "codexgateway init -force") {
		t.Fatalf("expected force hint, got: %q", output)
	}
	if !strings.Contains(output, "codexgateway start") {
		t.Fatalf("expected next-step hint, got: %q", output)
	}
}

func TestInit_WritesAPIKeyConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	input := strings.NewReader(strings.Join([]string{
		"",
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
	if cfg.Auth.APIKeys[0] == "" {
		t.Fatalf("expected generated local key, got: %#v", cfg.Auth.APIKeys)
	}
	if len(cfg.Auth.APIKeys[0]) < 24 {
		t.Fatalf("expected generated local key to be long enough, got %q", cfg.Auth.APIKeys[0])
	}
	if cfg.Upstream.Mode != "api_key" {
		t.Fatalf("unexpected mode: %q", cfg.Upstream.Mode)
	}
	if cfg.Upstream.APIKey != "sk-upstream-test" {
		t.Fatalf("unexpected upstream key: %q", cfg.Upstream.APIKey)
	}
	if !strings.Contains(out.String(), "本地网关 API Key") {
		t.Fatalf("expected local key hint in output: %q", out.String())
	}
	if !strings.Contains(out.String(), "codexgateway serve") {
		t.Fatalf("expected usage hint in output: %q", out.String())
	}
}

func TestInit_WritesOAuthConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	input := strings.NewReader(strings.Join([]string{
		"my-custom-key",
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
	if cfg.Auth.APIKeys[0] != "my-custom-key" {
		t.Fatalf("expected custom local key override, got %q", cfg.Auth.APIKeys[0])
	}
	if cfg.Upstream.APIKey != "" {
		t.Fatalf("expected empty upstream api key, got %q", cfg.Upstream.APIKey)
	}
	if !strings.Contains(out.String(), "oauth") {
		t.Fatalf("expected oauth default hint in output: %q", out.String())
	}
	if !strings.Contains(out.String(), "本地网关 API Key") {
		t.Fatalf("expected local key summary in output: %q", out.String())
	}
	if !strings.Contains(out.String(), "codexgateway auth login") {
		t.Fatalf("expected oauth next-step hint in output: %q", out.String())
	}
}

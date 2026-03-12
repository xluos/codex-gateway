package config

import (
	"strings"
	"testing"
)

func TestLoadConfig_RejectsMissingUpstreamAPIKey(t *testing.T) {
	_, err := LoadConfig(strings.NewReader(`
server:
  host: 127.0.0.1
  port: 8081
auth:
  api_keys:
    - local-key
upstream:
  base_url: https://api.openai.com
`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadConfig_LoadsValidConfig(t *testing.T) {
	cfg, err := LoadConfig(strings.NewReader(`
server:
  host: 127.0.0.1
  port: 8081
auth:
  api_keys:
    - local-key
upstream:
  base_url: https://api.openai.com
  api_key: sk-upstream
compat:
  enable_alias_routes: true
`))
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Server.Port != 8081 {
		t.Fatalf("unexpected port: %d", cfg.Server.Port)
	}
	if len(cfg.Auth.APIKeys) != 1 || cfg.Auth.APIKeys[0] != "local-key" {
		t.Fatalf("unexpected api keys: %#v", cfg.Auth.APIKeys)
	}
	if !cfg.Compat.EnableAliasRoutes {
		t.Fatal("expected alias routes enabled")
	}
}

func TestLoadConfig_LoadsOAuthMode(t *testing.T) {
	cfg, err := LoadConfig(strings.NewReader(`
upstream:
  mode: oauth
  base_url: https://api.openai.com
oauth:
  callback_host: 127.0.0.1
  callback_port: 1455
  callback_path: /auth/callback
  credentials_file: ./credentials/openai-oauth.json
auth:
  api_keys:
    - local-key
`))
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Upstream.Mode != "oauth" {
		t.Fatalf("unexpected mode: %q", cfg.Upstream.Mode)
	}
	if cfg.OAuth.CallbackPort != 1455 {
		t.Fatalf("unexpected callback port: %d", cfg.OAuth.CallbackPort)
	}
	if cfg.OAuth.CredentialsFile != "./credentials/openai-oauth.json" {
		t.Fatalf("unexpected credentials file: %q", cfg.OAuth.CredentialsFile)
	}
}

func TestLoadConfig_DefaultsOAuthCallbackHostToLocalhost(t *testing.T) {
	cfg, err := LoadConfig(strings.NewReader(`
upstream:
  mode: oauth
  base_url: https://api.openai.com
oauth:
  credentials_file: ./credentials/openai-oauth.json
auth:
  api_keys:
    - local-key
`))
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.OAuth.CallbackHost != "localhost" {
		t.Fatalf("unexpected callback host: %q", cfg.OAuth.CallbackHost)
	}
}

func TestLoadConfig_NormalizesLoopbackOAuthCallbackHostToLocalhost(t *testing.T) {
	cfg, err := LoadConfig(strings.NewReader(`
upstream:
  mode: oauth
  base_url: https://api.openai.com
oauth:
  callback_host: 127.0.0.1
  callback_port: 1455
  callback_path: /auth/callback
  credentials_file: ./credentials/openai-oauth.json
auth:
  api_keys:
    - local-key
`))
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.OAuth.CallbackHost != "localhost" {
		t.Fatalf("unexpected callback host: %q", cfg.OAuth.CallbackHost)
	}
}

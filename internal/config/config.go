package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Auth     AuthConfig     `yaml:"auth"`
	Upstream UpstreamConfig `yaml:"upstream"`
	OAuth    OAuthConfig    `yaml:"oauth"`
	Runtime  RuntimeConfig  `yaml:"runtime"`
	Logging  LoggingConfig  `yaml:"logging"`
	Compat   CompatConfig   `yaml:"compat"`
}

type ServerConfig struct {
	Host                string `yaml:"host"`
	Port                int    `yaml:"port"`
	ReadTimeoutSeconds  int    `yaml:"read_timeout_seconds"`
	WriteTimeoutSeconds int    `yaml:"write_timeout_seconds"`
}

type AuthConfig struct {
	APIKeys []string `yaml:"api_keys"`
}

type UpstreamConfig struct {
	Mode           string `yaml:"mode"`
	BaseURL        string `yaml:"base_url"`
	APIKey         string `yaml:"api_key"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

type OAuthConfig struct {
	CallbackHost    string `yaml:"callback_host"`
	CallbackPort    int    `yaml:"callback_port"`
	CallbackPath    string `yaml:"callback_path"`
	CredentialsFile string `yaml:"credentials_file"`
	AutoOpenBrowser bool   `yaml:"auto_open_browser"`
}

type RuntimeConfig struct {
	Dir       string `yaml:"dir"`
	PIDFile   string `yaml:"pid_file"`
	LogFile   string `yaml:"log_file"`
	StateFile string `yaml:"state_file"`
}

type LoggingConfig struct {
	Level         string `yaml:"level"`
	DebugDumpHTTP bool   `yaml:"debug_dump_http"`
}

type CompatConfig struct {
	EnableAliasRoutes bool `yaml:"enable_alias_routes"`
}

func LoadConfig(r io.Reader) (*Config, error) {
	var cfg Config
	if err := yaml.NewDecoder(r).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	applyDefaults(&cfg)
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if strings.TrimSpace(cfg.Server.Host) == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8081
	}
	if cfg.Upstream.TimeoutSeconds == 0 {
		cfg.Upstream.TimeoutSeconds = 600
	}
	if strings.TrimSpace(cfg.Upstream.Mode) == "" {
		cfg.Upstream.Mode = "api_key"
	}
	if cfg.Server.ReadTimeoutSeconds == 0 {
		cfg.Server.ReadTimeoutSeconds = 60
	}
	if cfg.Server.WriteTimeoutSeconds == 0 {
		cfg.Server.WriteTimeoutSeconds = 600
	}
	if strings.TrimSpace(cfg.OAuth.CallbackHost) == "" {
		cfg.OAuth.CallbackHost = "localhost"
	}
	cfg.OAuth.CallbackHost = normalizeOAuthCallbackHost(cfg.OAuth.CallbackHost)
	if cfg.OAuth.CallbackPort == 0 {
		cfg.OAuth.CallbackPort = 1455
	}
	if strings.TrimSpace(cfg.OAuth.CallbackPath) == "" {
		cfg.OAuth.CallbackPath = "/auth/callback"
	}
	defaultHomeDir := defaultCodexGatewayDir()
	if strings.TrimSpace(cfg.OAuth.CredentialsFile) == "" {
		cfg.OAuth.CredentialsFile = filepath.Join(defaultHomeDir, "openai-oauth.json")
	}
	if !cfg.OAuth.AutoOpenBrowser {
		cfg.OAuth.AutoOpenBrowser = true
	}
	if strings.TrimSpace(cfg.Runtime.Dir) == "" {
		cfg.Runtime.Dir = defaultHomeDir
	}
	cfg.Runtime.Dir = expandPath(cfg.Runtime.Dir)
	if strings.TrimSpace(cfg.Runtime.PIDFile) == "" {
		cfg.Runtime.PIDFile = filepath.Join(cfg.Runtime.Dir, "codex-gateway.pid")
	}
	if strings.TrimSpace(cfg.Runtime.LogFile) == "" {
		cfg.Runtime.LogFile = filepath.Join(cfg.Runtime.Dir, "codex-gateway.log")
	}
	if strings.TrimSpace(cfg.Runtime.StateFile) == "" {
		cfg.Runtime.StateFile = filepath.Join(cfg.Runtime.Dir, "codex-gateway.json")
	}
	cfg.OAuth.CredentialsFile = expandPath(cfg.OAuth.CredentialsFile)
	cfg.Runtime.PIDFile = expandPath(cfg.Runtime.PIDFile)
	cfg.Runtime.LogFile = expandPath(cfg.Runtime.LogFile)
	cfg.Runtime.StateFile = expandPath(cfg.Runtime.StateFile)
}

func validateConfig(cfg *Config) error {
	mode := strings.TrimSpace(cfg.Upstream.Mode)
	if mode != "api_key" && mode != "oauth" {
		return errors.New("upstream.mode must be one of: api_key, oauth")
	}
	if strings.TrimSpace(cfg.Upstream.BaseURL) == "" {
		return errors.New("upstream.base_url is required")
	}
	if mode == "api_key" && strings.TrimSpace(cfg.Upstream.APIKey) == "" {
		return errors.New("upstream.api_key is required")
	}
	if mode == "oauth" && strings.TrimSpace(cfg.OAuth.CredentialsFile) == "" {
		return errors.New("oauth.credentials_file is required")
	}
	if len(cfg.Auth.APIKeys) == 0 {
		return errors.New("auth.api_keys must not be empty")
	}
	for _, key := range cfg.Auth.APIKeys {
		if strings.TrimSpace(key) == "" {
			return errors.New("auth.api_keys must not contain empty values")
		}
	}
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return errors.New("server.port must be between 1 and 65535")
	}
	if cfg.OAuth.CallbackPort <= 0 || cfg.OAuth.CallbackPort > 65535 {
		return errors.New("oauth.callback_port must be between 1 and 65535")
	}
	return nil
}

func normalizeOAuthCallbackHost(host string) string {
	trimmed := strings.TrimSpace(strings.ToLower(host))
	switch trimmed {
	case "127.0.0.1", "::1", "[::1]":
		return "localhost"
	default:
		return strings.TrimSpace(host)
	}
}

func defaultCodexGatewayDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		home = "."
	}
	return filepath.Join(home, ".codex-gateway")
}

func expandPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return trimmed
	}
	if trimmed == "~" {
		return defaultCodexGatewayDir()
	}
	if strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err == nil && strings.TrimSpace(home) != "" {
			return filepath.Join(home, trimmed[2:])
		}
	}
	return trimmed
}

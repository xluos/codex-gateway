package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"codex-gateway/internal/config"
	"gopkg.in/yaml.v3"
)

const defaultConfigFileMode = 0o600

func Init(configPath string, force bool, in io.Reader, out io.Writer) error {
	if in == nil {
		return errors.New("input is required")
	}
	if out == nil {
		return errors.New("output is required")
	}

	resolvedConfigPath, err := resolvePath(configPath)
	if err != nil {
		return err
	}
	if !force {
		if _, err := os.Stat(resolvedConfigPath); err == nil {
			return fmt.Errorf("config already exists: %s", resolvedConfigPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat config: %w", err)
		}
	}

	reader := bufio.NewReader(in)
	configDir := filepath.Dir(resolvedConfigPath)

	_, _ = fmt.Fprintf(out, "Initializing codex-gateway in %s\n\n", configDir)

	localAPIKey, err := promptRequired(reader, out, "Local gateway API key (used by your clients): ")
	if err != nil {
		return err
	}

	mode, err := promptMode(reader, out)
	if err != nil {
		return err
	}

	upstreamAPIKey := ""
	if mode == "api_key" {
		upstreamAPIKey, err = promptRequired(reader, out, "OpenAI upstream API key: ")
		if err != nil {
			return err
		}
	}

	cfg := buildInitConfig(configDir, localAPIKey, mode, upstreamAPIKey)
	if err := confirmWrite(reader, out, resolvedConfigPath, cfg); err != nil {
		return err
	}

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(resolvedConfigPath, data, defaultConfigFileMode); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	printInitNextSteps(out, resolvedConfigPath, cfg)
	return nil
}

func buildInitConfig(configDir, localAPIKey, mode, upstreamAPIKey string) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Host:                "127.0.0.1",
			Port:                9867,
			ReadTimeoutSeconds:  60,
			WriteTimeoutSeconds: 600,
		},
		Auth: config.AuthConfig{
			APIKeys: []string{localAPIKey},
		},
		Upstream: config.UpstreamConfig{
			Mode:           mode,
			BaseURL:        "https://api.openai.com",
			APIKey:         upstreamAPIKey,
			TimeoutSeconds: 600,
		},
		OAuth: config.OAuthConfig{
			CallbackHost:    "localhost",
			CallbackPort:    1455,
			CallbackPath:    "/auth/callback",
			CredentialsFile: filepath.Join(configDir, "openai-oauth.json"),
			AutoOpenBrowser: true,
		},
		Runtime: config.RuntimeConfig{
			Dir:       configDir,
			PIDFile:   filepath.Join(configDir, "codex-gateway.pid"),
			LogFile:   filepath.Join(configDir, "codex-gateway.log"),
			StateFile: filepath.Join(configDir, "codex-gateway.json"),
		},
		Logging: config.LoggingConfig{
			Level:         "info",
			DebugDumpHTTP: false,
		},
		Compat: config.CompatConfig{
			EnableAliasRoutes: true,
		},
	}
}

func promptRequired(reader *bufio.Reader, out io.Writer, prompt string) (string, error) {
	for {
		_, _ = io.WriteString(out, prompt)
		value, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read input: %w", err)
		}
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed, nil
		}
		if errors.Is(err, io.EOF) {
			return "", errors.New("missing required input")
		}
		_, _ = io.WriteString(out, "This field is required.\n")
	}
}

func promptMode(reader *bufio.Reader, out io.Writer) (string, error) {
	for {
		_, _ = io.WriteString(out, "Upstream mode:\n")
		_, _ = io.WriteString(out, "  1) api_key\n")
		_, _ = io.WriteString(out, "  2) oauth\n")
		_, _ = io.WriteString(out, "Choose upstream mode [1/2]: ")

		value, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read input: %w", err)
		}
		switch strings.TrimSpace(value) {
		case "1", "api_key":
			return "api_key", nil
		case "2", "oauth":
			return "oauth", nil
		case "":
			if errors.Is(err, io.EOF) {
				return "", errors.New("missing upstream mode")
			}
		}
		if errors.Is(err, io.EOF) {
			return "", errors.New("invalid upstream mode")
		}
		_, _ = io.WriteString(out, "Please enter 1 or 2.\n")
	}
}

func confirmWrite(reader *bufio.Reader, out io.Writer, configPath string, cfg *config.Config) error {
	_, _ = fmt.Fprintf(out, "\nConfig path: %s\n", configPath)
	_, _ = fmt.Fprintf(out, "Runtime dir: %s\n", cfg.Runtime.Dir)
	_, _ = fmt.Fprintf(out, "Credentials: %s\n", cfg.OAuth.CredentialsFile)
	_, _ = fmt.Fprintf(out, "Continue and write config? [Y/n]: ")

	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "y", "yes":
		return nil
	default:
		return errors.New("initialization cancelled")
	}
}

func printInitNextSteps(out io.Writer, configPath string, cfg *config.Config) {
	_, _ = fmt.Fprintf(out, "\nConfig written: %s\n", configPath)
	_, _ = fmt.Fprintf(out, "Runtime files will be stored in: %s\n", cfg.Runtime.Dir)
	_, _ = io.WriteString(out, "\nNext steps:\n")
	if cfg.Upstream.Mode == "oauth" {
		_, _ = io.WriteString(out, "  1. codexgateway auth login\n")
		_, _ = io.WriteString(out, "  2. codexgateway start\n")
		_, _ = io.WriteString(out, "  3. codexgateway status\n")
		_, _ = io.WriteString(out, "  4. codexgateway logs -n 100\n")
		return
	}
	_, _ = io.WriteString(out, "  1. codexgateway serve\n")
	_, _ = io.WriteString(out, "  2. codexgateway start\n")
	_, _ = io.WriteString(out, "  3. codexgateway status\n")
	_, _ = io.WriteString(out, "  4. codexgateway logs -n 100\n")
}

func resolvePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("config path is required")
	}
	if trimmed == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		return home, nil
	}
	if strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		return filepath.Join(home, trimmed[2:]), nil
	}
	return trimmed, nil
}

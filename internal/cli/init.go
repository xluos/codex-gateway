package cli

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
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
			printExistingConfigMessage(out, resolvedConfigPath)
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat config: %w", err)
		}
	}

	reader := bufio.NewReader(in)
	configDir := filepath.Dir(resolvedConfigPath)
	defaultLocalAPIKey, err := generateLocalAPIKey()
	if err != nil {
		return fmt.Errorf("generate local api key: %w", err)
	}

	_, _ = fmt.Fprintf(out, "正在初始化 codex-gateway，配置目录：%s\n\n", configDir)

	localAPIKey, err := promptWithDefault(reader, out, "本地网关 API Key（直接回车使用默认值）", defaultLocalAPIKey)
	if err != nil {
		return err
	}

	mode, err := promptMode(reader, out)
	if err != nil {
		return err
	}

	upstreamAPIKey := ""
	if mode == "api_key" {
		upstreamAPIKey, err = promptRequired(reader, out, "OpenAI 上游 API Key: ")
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
		_, _ = io.WriteString(out, "该项为必填，请重新输入。\n")
	}
}

func promptWithDefault(reader *bufio.Reader, out io.Writer, label, defaultValue string) (string, error) {
	for {
		_, _ = fmt.Fprintf(out, "%s: %s\n", label, defaultValue)
		_, _ = io.WriteString(out, "输入新的值可覆盖，直接回车则接受默认值：")

		value, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read input: %w", err)
		}
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed, nil
		}
		if strings.TrimSpace(defaultValue) != "" {
			return defaultValue, nil
		}
		if errors.Is(err, io.EOF) {
			return "", errors.New("missing required input")
		}
		_, _ = io.WriteString(out, "该项为必填，请重新输入。\n")
	}
}

func promptMode(reader *bufio.Reader, out io.Writer) (string, error) {
	for {
		_, _ = io.WriteString(out, "请选择上游认证方式：\n")
		_, _ = io.WriteString(out, "  1) api_key\n")
		_, _ = io.WriteString(out, "  2) oauth（默认）\n")
		_, _ = io.WriteString(out, "请输入选项 [1/2，直接回车使用 oauth]: ")

		value, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read input: %w", err)
		}
		switch strings.TrimSpace(value) {
		case "1", "api_key":
			return "api_key", nil
		case "", "2", "oauth":
			return "oauth", nil
		}
		if errors.Is(err, io.EOF) {
			return "", errors.New("invalid upstream mode")
		}
		_, _ = io.WriteString(out, "请输入 1 或 2。\n")
	}
}

func confirmWrite(reader *bufio.Reader, out io.Writer, configPath string, cfg *config.Config) error {
	_, _ = fmt.Fprintf(out, "\n配置文件路径：%s\n", configPath)
	_, _ = fmt.Fprintf(out, "运行时目录：%s\n", cfg.Runtime.Dir)
	_, _ = fmt.Fprintf(out, "OAuth 凭证文件：%s\n", cfg.OAuth.CredentialsFile)
	_, _ = fmt.Fprintf(out, "确认写入配置？[Y/n]: ")

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
	_, _ = fmt.Fprintf(out, "\n配置已写入：%s\n", configPath)
	_, _ = fmt.Fprintf(out, "运行时文件目录：%s\n", cfg.Runtime.Dir)
	if len(cfg.Auth.APIKeys) > 0 {
		_, _ = fmt.Fprintf(out, "本地网关 API Key：%s\n", cfg.Auth.APIKeys[0])
		_, _ = io.WriteString(out, "请把这个 Key 配到你的客户端里，后续请求网关时使用。\n")
	}
	_, _ = io.WriteString(out, "\n接下来可以这样使用：\n")
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

func printExistingConfigMessage(out io.Writer, configPath string) {
	_, _ = fmt.Fprintf(out, "已检测到现有配置文件：%s\n", configPath)
	_, _ = io.WriteString(out, "当前无需重新初始化。\n\n")
	_, _ = io.WriteString(out, "你可以直接使用：\n")
	_, _ = io.WriteString(out, "  codexgateway start\n")
	_, _ = io.WriteString(out, "  codexgateway status\n")
	_, _ = io.WriteString(out, "  codexgateway logs -n 100\n\n")
	_, _ = io.WriteString(out, "如果你确认要覆盖现有配置，请执行：\n")
	_, _ = io.WriteString(out, "  codexgateway init -force\n")
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

func generateLocalAPIKey() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "cgw-" + hex.EncodeToString(buf), nil
}

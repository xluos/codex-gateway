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
	"codex-gateway/internal/ui"

	"github.com/AlecAivazis/survey/v2"
	"golang.org/x/term"
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
	if isInteractiveTerminal(in, out) {
		return initWithSurvey(resolvedConfigPath, configDir, defaultLocalAPIKey, out)
	}

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
	ui.PrintLines(out, "", ui.Success("配置已写入"), ui.KV("配置文件", configPath), ui.KV("运行时目录", cfg.Runtime.Dir))
	if len(cfg.Auth.APIKeys) > 0 {
		ui.PrintLines(out, ui.KV("本地网关 API Key", cfg.Auth.APIKeys[0]), ui.Muted("请把这个 Key 配到你的客户端里，后续请求网关时使用。"))
	}
	ui.PrintLines(out, ui.Section("接下来可以这样使用"))
	if cfg.Upstream.Mode == "oauth" {
		ui.PrintLines(out, "  1. codexgateway auth login", "  2. codexgateway start", "  3. codexgateway status", "  4. codexgateway logs -n 100")
		return
	}
	ui.PrintLines(out, "  1. codexgateway serve", "  2. codexgateway start", "  3. codexgateway status", "  4. codexgateway logs -n 100")
}

func printExistingConfigMessage(out io.Writer, configPath string) {
	ui.PrintLines(out,
		ui.Warn("已检测到现有配置文件"),
		ui.KV("配置文件", configPath),
		ui.Muted("当前无需重新初始化。"),
		ui.Section("你可以直接使用"),
		"  codexgateway start",
		"  codexgateway status",
		"  codexgateway logs -n 100",
		"",
		ui.Section("如果你确认要覆盖现有配置"),
		"  codexgateway init --force",
	)
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

func isInteractiveTerminal(in io.Reader, out io.Writer) bool {
	inFile, ok := in.(*os.File)
	if !ok {
		return false
	}
	outFile, ok := out.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(inFile.Fd())) && term.IsTerminal(int(outFile.Fd()))
}

func initWithSurvey(configPath, configDir, defaultLocalAPIKey string, out io.Writer) error {
	ui.PrintLines(out, ui.Banner(out), ui.Section("初始化向导"), ui.KV("配置目录", configDir))

	answers := struct {
		LocalAPIKey string
		Mode        string
		UpstreamKey string
		Confirm     bool
	}{
		LocalAPIKey: defaultLocalAPIKey,
		Mode:        "oauth",
	}

	questions := []*survey.Question{
		{
			Name: "LocalAPIKey",
			Prompt: &survey.Input{
				Message: "本地网关 API Key（回车接受默认值）",
				Default: defaultLocalAPIKey,
			},
			Validate: survey.Required,
		},
		{
			Name: "Mode",
			Prompt: &survey.Select{
				Message: "请选择上游认证方式",
				Options: []string{"oauth", "api_key"},
				Default: "oauth",
			},
		},
	}
	if err := survey.Ask(questions, &answers); err != nil {
		return err
	}
	if answers.Mode == "api_key" {
		if err := survey.AskOne(&survey.Password{Message: "OpenAI 上游 API Key"}, &answers.UpstreamKey, survey.WithValidator(survey.Required)); err != nil {
			return err
		}
	}

	cfg := buildInitConfig(configDir, answers.LocalAPIKey, answers.Mode, answers.UpstreamKey)
	ui.PrintLines(out, ui.Section("即将写入"), ui.KV("配置文件", configPath), ui.KV("运行时目录", cfg.Runtime.Dir), ui.KV("OAuth 凭证文件", cfg.OAuth.CredentialsFile))
	if err := survey.AskOne(&survey.Confirm{Message: "确认写入配置？", Default: true}, &answers.Confirm); err != nil {
		return err
	}
	if !answers.Confirm {
		return errors.New("initialization cancelled")
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(configPath, data, defaultConfigFileMode); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	printInitNextSteps(out, configPath, cfg)
	return nil
}

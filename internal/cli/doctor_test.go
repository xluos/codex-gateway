package cli

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-gateway/internal/config"
	"codex-gateway/internal/oauth"
)

func reserveTCPPort(t *testing.T) (string, int) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer listener.Close()

	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort returned error: %v", err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatalf("LookupPort returned error: %v", err)
	}
	return host, port
}

func TestDoctor_ReportsMissingConfig(t *testing.T) {
	var out bytes.Buffer
	err := Doctor(filepath.Join(t.TempDir(), "missing.yaml"), &out)
	if err == nil {
		t.Fatal("expected doctor to report error")
	}
	text := out.String()
	if !strings.Contains(text, "配置文件不存在") {
		t.Fatalf("unexpected output: %q", text)
	}
	if !strings.Contains(text, "codexgateway init") {
		t.Fatalf("expected init hint in output: %q", text)
	}
}

func TestDoctor_ReportsHealthyOAuthSetup(t *testing.T) {
	host, port := reserveTCPPort(t)

	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	credPath := filepath.Join(baseDir, "openai-oauth.json")
	runtimeDir := filepath.Join(baseDir, "runtime")

	cfgText := `
server:
  host: ` + host + `
  port: ` + fmt.Sprintf("%d", port) + `
auth:
  api_keys:
    - local-key
upstream:
  mode: oauth
  base_url: https://api.openai.com
oauth:
  credentials_file: ` + credPath + `
runtime:
  dir: ` + runtimeDir + `
  pid_file: ` + filepath.Join(runtimeDir, "codex-gateway.pid") + `
  log_file: ` + filepath.Join(runtimeDir, "codex-gateway.log") + `
  state_file: ` + filepath.Join(runtimeDir, "codex-gateway.json") + `
`
	if err := os.WriteFile(configPath, []byte(cfgText), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	store := oauth.NewStore(credPath)
	if err := store.Save(&oauth.Credentials{
		AccessToken:  "at",
		RefreshToken: "rt",
		Email:        "doctor@example.com",
		PlanType:     "plus",
		ExpiresAt:    time.Now().Add(2 * time.Hour).Unix(),
	}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	var out bytes.Buffer
	if err := Doctor(configPath, &out); err != nil {
		t.Fatalf("Doctor returned error: %v", err)
	}

	text := out.String()
	for _, want := range []string{
		"配置文件可用",
		"OAuth 凭证可用",
		"doctor@example.com",
		"运行目录可写",
		"服务当前未运行",
		"监听端口可用",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected doctor output to contain %q, got %q", want, text)
		}
	}
}

func TestDoctor_ReportsStaleRuntimeState(t *testing.T) {
	baseDir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "127.0.0.1",
			Port: 9867,
		},
		Auth: config.AuthConfig{
			APIKeys: []string{"local-key"},
		},
		Upstream: config.UpstreamConfig{
			Mode:    "api_key",
			BaseURL: "https://api.openai.com",
			APIKey:  "sk-test",
		},
		Runtime: config.RuntimeConfig{
			Dir:       filepath.Join(baseDir, "runtime"),
			PIDFile:   filepath.Join(baseDir, "runtime", "codex-gateway.pid"),
			LogFile:   filepath.Join(baseDir, "runtime", "codex-gateway.log"),
			StateFile: filepath.Join(baseDir, "runtime", "codex-gateway.json"),
		},
	}
	if err := os.MkdirAll(cfg.Runtime.Dir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	configPath := filepath.Join(baseDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
auth:
  api_keys:
    - local-key
upstream:
  base_url: https://api.openai.com
  api_key: sk-test
runtime:
  dir: `+cfg.Runtime.Dir+`
  pid_file: `+cfg.Runtime.PIDFile+`
  log_file: `+cfg.Runtime.LogFile+`
  state_file: `+cfg.Runtime.StateFile+`
server:
  host: 127.0.0.1
  port: 9867
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := writeState(cfg.Runtime.StateFile, State{
		PID:        999999,
		Address:    "127.0.0.1:9867",
		LogFile:    cfg.Runtime.LogFile,
		ConfigPath: configPath,
		StartedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("writeState returned error: %v", err)
	}

	var out bytes.Buffer
	err := Doctor(configPath, &out)
	if err == nil {
		t.Fatal("expected doctor to report stale state issue")
	}
	if !strings.Contains(out.String(), "检测到过期状态文件") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestDoctor_DoesNotReportPortUnavailableWhenServiceIsRunning(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort returned error: %v", err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatalf("LookupPort returned error: %v", err)
	}

	baseDir := t.TempDir()
	runtimeDir := filepath.Join(baseDir, "runtime")
	configPath := filepath.Join(baseDir, "config.yaml")
	pidFile := filepath.Join(runtimeDir, "codex-gateway.pid")
	logFile := filepath.Join(runtimeDir, "codex-gateway.log")
	stateFile := filepath.Join(runtimeDir, "codex-gateway.json")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	configText := fmt.Sprintf(`
auth:
  api_keys:
    - local-key
upstream:
  base_url: https://api.openai.com
  api_key: sk-test
runtime:
  dir: %s
  pid_file: %s
  log_file: %s
  state_file: %s
server:
  host: %s
  port: %d
`, runtimeDir, pidFile, logFile, stateFile, host, port)
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := writeState(stateFile, State{
		PID:        os.Getpid(),
		Address:    addr,
		LogFile:    logFile,
		ConfigPath: configPath,
		StartedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("writeState returned error: %v", err)
	}

	var out bytes.Buffer
	err = Doctor(configPath, &out)
	if err != nil {
		t.Fatalf("Doctor returned error: %v\noutput: %s", err, out.String())
	}
	if strings.Contains(out.String(), "监听端口不可用") {
		t.Fatalf("expected doctor not to report port unavailable, got: %q", out.String())
	}
}

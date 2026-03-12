package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"codex-gateway/internal/config"
)

const (
	backgroundEnvVar = "OPENAI_LOCAL_GATEWAY_STDOUT"
	stateVersion     = 1
)

func BackgroundStdoutEnvVar() string {
	return backgroundEnvVar
}

type State struct {
	Version    int       `json:"version"`
	PID        int       `json:"pid"`
	Address    string    `json:"address"`
	LogFile    string    `json:"log_file"`
	ConfigPath string    `json:"config_path"`
	StartedAt  time.Time `json:"started_at"`
}

func writeUsage(w io.Writer) {
	_, _ = fmt.Fprintf(w, strings.TrimSpace(`
Codex Gateway

Usage:
  server serve   -config ~/.codex-gateway/config.yaml
  server start   -config ~/.codex-gateway/config.yaml
  server stop    -config ~/.codex-gateway/config.yaml
  server restart -config ~/.codex-gateway/config.yaml
  server status  -config ~/.codex-gateway/config.yaml
  server logs    -config ~/.codex-gateway/config.yaml [-n 100]
  server help

Auth:
  server auth login   -config ~/.codex-gateway/config.yaml
  server auth status  -config ~/.codex-gateway/config.yaml
  server auth refresh -config ~/.codex-gateway/config.yaml

Notes:
  - 'serve' runs in the foreground.
  - if -config is omitted, the default config path is ~/.codex-gateway/config.yaml.
  - 'start' launches a background process and writes runtime state into ~/.codex-gateway by default.
  - logs are written to the configured runtime log file.
`))
	_, _ = io.WriteString(w, "\n")
}

func Help(w io.Writer) error {
	writeUsage(w)
	return nil
}

func Start(ctx context.Context, configPath string, cfg *config.Config, w io.Writer) error {
	if cfg == nil {
		return errors.New("config is required")
	}
	if err := os.MkdirAll(cfg.Runtime.Dir, 0o755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}

	if state, err := readState(cfg.Runtime.StateFile); err == nil && processExists(state.PID) {
		_, _ = fmt.Fprintf(w, "already running: pid=%d addr=%s\n", state.PID, state.Address)
		return nil
	}
	_ = removeRuntimeFiles(cfg)

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	logFile, err := os.OpenFile(cfg.Runtime.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open runtime log file: %w", err)
	}
	defer logFile.Close()

	cmd := exec.CommandContext(ctx, exePath, "serve", "-config", configPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), backgroundEnvVar+"=0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start background process: %w", err)
	}

	if err := waitForHealthy(cfg, 10*time.Second); err != nil {
		_ = syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)
		logTail, _ := tailFile(cfg.Runtime.LogFile, 20)
		return fmt.Errorf("background process failed to become healthy: %w\nrecent logs:\n%s", err, logTail)
	}

	state, err := readState(cfg.Runtime.StateFile)
	if err != nil {
		return fmt.Errorf("read runtime state after start: %w", err)
	}
	_, _ = fmt.Fprintf(w, "started: pid=%d addr=%s log=%s\n", state.PID, state.Address, state.LogFile)
	return nil
}

func Stop(cfg *config.Config, w io.Writer) error {
	if cfg == nil {
		return errors.New("config is required")
	}
	state, err := readState(cfg.Runtime.StateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(w, "already stopped\n")
			return nil
		}
		return err
	}
	if !processExists(state.PID) {
		_ = removeRuntimeFiles(cfg)
		_, _ = fmt.Fprintf(w, "stopped stale state: pid=%d\n", state.PID)
		return nil
	}

	if err := syscall.Kill(state.PID, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("signal process %d: %w", state.PID, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(state.PID) {
			_ = removeRuntimeFiles(cfg)
			_, _ = fmt.Fprintf(w, "stopped: pid=%d\n", state.PID)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := syscall.Kill(state.PID, syscall.SIGKILL); err != nil {
		return fmt.Errorf("force kill process %d: %w", state.PID, err)
	}
	_ = removeRuntimeFiles(cfg)
	_, _ = fmt.Fprintf(w, "killed: pid=%d\n", state.PID)
	return nil
}

func Restart(ctx context.Context, configPath string, cfg *config.Config, w io.Writer) error {
	if err := Stop(cfg, io.Discard); err != nil {
		return err
	}
	return Start(ctx, configPath, cfg, w)
}

func Status(cfg *config.Config, w io.Writer) error {
	if cfg == nil {
		return errors.New("config is required")
	}
	state, err := readState(cfg.Runtime.StateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(w, "status: stopped\nlog_file: %s\npid_file: %s\n", cfg.Runtime.LogFile, cfg.Runtime.PIDFile)
			return nil
		}
		return err
	}
	running := processExists(state.PID)
	status := "stopped"
	if running {
		status = "running"
	}
	_, _ = fmt.Fprintf(w, "status: %s\npid: %d\naddr: %s\nstarted_at: %s\nconfig: %s\nlog_file: %s\npid_file: %s\n",
		status, state.PID, state.Address, state.StartedAt.Format(time.RFC3339), state.ConfigPath, state.LogFile, cfg.Runtime.PIDFile)
	if !running {
		_, _ = io.WriteString(w, "note: stale runtime state detected\n")
	}
	return nil
}

func Logs(cfg *config.Config, w io.Writer, lines int) error {
	if cfg == nil {
		return errors.New("config is required")
	}
	content, err := tailFile(cfg.Runtime.LogFile, lines)
	if err != nil {
		return err
	}
	_, _ = io.WriteString(w, content)
	return nil
}

func WriteRuntimeState(cfg *config.Config, configPath, addr string, pid int) error {
	if cfg == nil {
		return errors.New("config is required")
	}
	if err := os.MkdirAll(cfg.Runtime.Dir, 0o755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	state := State{
		Version:    stateVersion,
		PID:        pid,
		Address:    addr,
		LogFile:    cfg.Runtime.LogFile,
		ConfigPath: configPath,
		StartedAt:  time.Now().UTC(),
	}
	if err := writeState(cfg.Runtime.StateFile, state); err != nil {
		return err
	}
	return os.WriteFile(cfg.Runtime.PIDFile, []byte(fmt.Sprintf("%d\n", pid)), 0o600)
}

func RemoveRuntimeState(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	return removeRuntimeFiles(cfg)
}

func readState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

func writeState(path string, state State) error {
	state.Version = stateVersion
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func removeRuntimeFiles(cfg *config.Config) error {
	var firstErr error
	for _, path := range []string{cfg.Runtime.StateFile, cfg.Runtime.PIDFile} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func tailFile(path string, lines int) (string, error) {
	if lines <= 0 {
		lines = 100
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	all := strings.SplitAfter(string(data), "\n")
	if len(all) > 0 && all[len(all)-1] == "" {
		all = all[:len(all)-1]
	}
	if len(all) <= lines {
		return strings.Join(all, ""), nil
	}
	return strings.Join(all[len(all)-lines:], ""), nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

func waitForHealthy(cfg *config.Config, timeout time.Duration) error {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	url := fmt.Sprintf("http://%s:%d/healthz", cfg.Server.Host, cfg.Server.Port)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("health check timed out for %s", url)
}

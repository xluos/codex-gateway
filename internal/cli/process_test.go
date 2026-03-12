package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-gateway/internal/config"
)

func TestWriteUsage_IncludesProcessCommands(t *testing.T) {
	var out bytes.Buffer
	writeUsage(&out)

	text := out.String()
	for _, want := range []string{
		"serve",
		"start",
		"stop",
		"restart",
		"status",
		"logs",
		"help",
		"auth login",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected usage to contain %q, got %q", want, text)
		}
	}
}

func TestReadState_ReturnsSavedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := State{
		PID:        os.Getpid(),
		Address:    "127.0.0.1:9867",
		LogFile:    "./runtime/codex-gateway.log",
		ConfigPath: "./config.yaml",
		StartedAt:  time.Unix(1700000000, 0).UTC(),
	}
	if err := writeState(path, want); err != nil {
		t.Fatalf("writeState returned error: %v", err)
	}

	got, err := readState(path)
	if err != nil {
		t.Fatalf("readState returned error: %v", err)
	}
	if got.PID != want.PID || got.Address != want.Address || got.LogFile != want.LogFile || got.ConfigPath != want.ConfigPath {
		t.Fatalf("unexpected state: %#v", got)
	}
}

func TestTailFile_ReturnsLastNLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	lines, err := tailFile(path, 2)
	if err != nil {
		t.Fatalf("tailFile returned error: %v", err)
	}
	if lines != "two\nthree\n" {
		t.Fatalf("unexpected tailed lines: %q", lines)
	}
}

func TestProcessExists_RejectsInvalidPID(t *testing.T) {
	if processExists(999999) {
		t.Fatal("expected invalid pid to be reported as not running")
	}
	if !processExists(os.Getpid()) {
		t.Fatal("expected current pid to be reported as running")
	}
}

func TestStateSummary_ReportsMissingState(t *testing.T) {
	cfg := &config.Config{
		Runtime: config.RuntimeConfig{
			PIDFile:   filepath.Join(t.TempDir(), "gateway.pid"),
			LogFile:   filepath.Join(t.TempDir(), "gateway.log"),
			StateFile: filepath.Join(t.TempDir(), "gateway.json"),
		},
	}
	var out bytes.Buffer

	if err := Status(cfg, &out); err != nil {
		t.Fatalf("Status returned error: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "status: stopped") {
		t.Fatalf("unexpected status output: %q", text)
	}
	if !strings.Contains(text, cfg.Runtime.LogFile) {
		t.Fatalf("expected log file in status output: %q", text)
	}
}

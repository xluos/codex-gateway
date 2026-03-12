package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunHelp_PrintsCLIOverview(t *testing.T) {
	var out bytes.Buffer

	if err := run([]string{"help"}, &out); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	text := out.String()
	for _, want := range []string{
		"Codex Gateway",
		"init",
		"doctor",
		"auth login",
		"start",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected help output to contain %q, got %q", want, text)
		}
	}
}

func TestRunHelp_IncludesCompletionCommand(t *testing.T) {
	var out bytes.Buffer

	err := run([]string{"help"}, &out)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	text := out.String()
	for _, want := range []string{
		"completion",
		"completion zsh|bash|fish",
		"shell 补全脚本",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected help output to contain %q, got %q", want, text)
		}
	}
}

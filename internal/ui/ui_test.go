package ui

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestBannerForWriter_UsesPlainBannerWhenNO_COLORIsSet(t *testing.T) {
	var out bytes.Buffer

	got := bannerForWriter(&out, func(key string) string {
		if key == "NO_COLOR" {
			return "1"
		}
		return ""
	}, func(_ io.Writer) bool {
		return true
	})

	if !strings.HasPrefix(got, bannerPlain) {
		t.Fatalf("expected plain banner when NO_COLOR is set")
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("expected plain banner without ANSI escape sequences, got %q", got)
	}
	if !strings.Contains(got, "Codex Gateway") {
		t.Fatalf("expected banner to include project title")
	}
}

func TestBannerForWriter_UsesPlainBannerForNonTTYWriter(t *testing.T) {
	var out bytes.Buffer

	got := bannerForWriter(&out, func(string) string { return "" }, func(_ io.Writer) bool {
		return false
	})

	if !strings.HasPrefix(got, bannerPlain) {
		t.Fatalf("expected plain banner for non-TTY writer")
	}
}

func TestBannerForWriter_UsesANSIBannerForTTYWriter(t *testing.T) {
	var out bytes.Buffer

	got := bannerForWriter(&out, func(string) string { return "" }, func(_ io.Writer) bool {
		return true
	})

	if !strings.HasPrefix(got, bannerANSI) {
		t.Fatalf("expected ANSI banner for TTY writer")
	}
	if !strings.Contains(got, "\x1b[38;2;") {
		t.Fatalf("expected ANSI banner to contain truecolor escape sequences")
	}
}

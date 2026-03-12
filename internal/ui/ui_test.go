package ui

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

const expectedPlainBanner = "" +
	"   ____          __           \n" +
	"  / ___|___   __/ /___  _  __\n" +
	" | |   / _ \\ /_  / _ \\| |/_/\n" +
	" | |__| (_) | / /  __/>  <  \n" +
	"  \\____\\___/ /_/ \\___/_/|_|  \n" +
	"\n" +
	"    ____       __                                 \n" +
	"   / ___|___ _/ /____ _      ______ ___  _______ \n" +
	"  / / __/ _ `/ __/ _ \\ | /| / / __ `/ / / / _ \\\n" +
	" / /_/ / (_/ / /_/  __/ |/ |/ / /_/ / /_/ /  __/\n" +
	" \\____/\\__,_/\\__/\\___/|__/|__/\\__,_/\\__, /\\___/ \n" +
	"                                   /____/       \n"

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

func TestBannerAssets_MatchSelectedWordmark(t *testing.T) {
	if bannerPlain != expectedPlainBanner {
		t.Fatalf("plain banner does not match approved wordmark")
	}
	if !strings.Contains(bannerANSI, "\x1b[38;2;80;200;255m   ____          __") {
		t.Fatalf("ansi banner does not start with the approved gradient wordmark")
	}
	if strings.Contains(bannerANSI, "█") {
		t.Fatalf("ansi banner should not use block characters")
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

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-gateway/internal/config"
	"codex-gateway/internal/oauth"
)

func TestAuthStatus_PrintsCredentialSummary(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "oauth.json")
	store := oauth.NewStore(storePath)
	if err := store.Save(&oauth.Credentials{
		AccessToken:  "at",
		RefreshToken: "rt",
		Email:        "user@example.com",
		PlanType:     "plus",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	cfg := &config.Config{
		OAuth: config.OAuthConfig{
			CredentialsFile: storePath,
		},
	}
	var out bytes.Buffer

	if err := AuthStatus(cfg, &out); err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "user@example.com") {
		t.Fatalf("missing email in output: %q", output)
	}
	if !strings.Contains(output, "plus") {
		t.Fatalf("missing plan type in output: %q", output)
	}
}

func TestAuthLogin_PrintsAuthorizationURLWhenAutoOpenDisabled(t *testing.T) {
	cfg := &config.Config{
		OAuth: config.OAuthConfig{
			CallbackHost:    "localhost",
			CallbackPort:    0,
			CallbackPath:    "/auth/callback",
			CredentialsFile: filepath.Join(t.TempDir(), "oauth.json"),
			AutoOpenBrowser: false,
		},
	}
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := AuthLogin(ctx, cfg, strings.NewReader(""), &out, "")
	if err == nil {
		t.Fatal("expected error because context is canceled")
	}
	output := out.String()
	if !strings.Contains(output, "https://auth.openai.com/oauth/authorize?") {
		t.Fatalf("expected authorization URL in output, got: %q", output)
	}
}

func TestAuthLogin_UsesConfiguredRedirectURIInsteadOfListenerAddress(t *testing.T) {
	cfg := &config.Config{
		OAuth: config.OAuthConfig{
			CallbackHost:    "localhost",
			CallbackPort:    0,
			CallbackPath:    "/auth/callback",
			CredentialsFile: filepath.Join(t.TempDir(), "oauth.json"),
			AutoOpenBrowser: false,
		},
	}
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = AuthLogin(ctx, cfg, strings.NewReader(""), &out, "")

	output := out.String()
	if !strings.Contains(output, "redirect_uri=http%3A%2F%2Flocalhost%3A0%2Fauth%2Fcallback") {
		t.Fatalf("expected configured redirect_uri in authorization URL, got: %q", output)
	}
}

func TestOAuthAccountStatuses(t *testing.T) {
	tmpDir := t.TempDir()
	validPath := filepath.Join(tmpDir, "valid.json")
	expiredPath := filepath.Join(tmpDir, "expired.json")
	if err := oauth.NewStore(validPath).Save(&oauth.Credentials{
		AccessToken:  "at",
		RefreshToken: "rt",
		Email:        "valid@example.com",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatalf("save valid creds: %v", err)
	}
	if err := oauth.NewStore(expiredPath).Save(&oauth.Credentials{
		AccessToken:  "at",
		RefreshToken: "rt",
		Email:        "expired@example.com",
		ExpiresAt:    time.Now().Add(-time.Hour).Unix(),
	}); err != nil {
		t.Fatalf("save expired creds: %v", err)
	}

	cfg := &config.Config{
		Upstreams: []config.NamedUpstreamConfig{
			{Name: "valid", Mode: "oauth", DefaultModel: "gpt-5.1-codex-mini", OAuth: config.UpstreamOAuthConfig{CredentialsFile: validPath}},
			{Name: "expired", Mode: "oauth", DefaultModel: "gpt-5.1-codex-mini", OAuth: config.UpstreamOAuthConfig{CredentialsFile: expiredPath}},
			{Name: "missing", Mode: "oauth", DefaultModel: "gpt-5.1-codex-mini", OAuth: config.UpstreamOAuthConfig{CredentialsFile: filepath.Join(tmpDir, "missing.json")}},
		},
	}

	statuses := discoverOAuthAccountStatuses(cfg)
	if len(statuses) != 3 {
		t.Fatalf("unexpected status count: %d", len(statuses))
	}
	if statuses[0].Status != "valid" || statuses[1].Status != "expired" || statuses[2].Status != "missing" {
		t.Fatalf("unexpected statuses: %#v", statuses)
	}
}

func TestAuthLogin_PromptsForAccountSelection(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		OAuth: config.OAuthConfig{
			CallbackHost:    "localhost",
			CallbackPort:    0,
			CallbackPath:    "/auth/callback",
			AutoOpenBrowser: false,
		},
		Upstreams: []config.NamedUpstreamConfig{
			{Name: "primary", Mode: "oauth", DefaultModel: "gpt-5.1-codex-mini", OAuth: config.UpstreamOAuthConfig{CredentialsFile: filepath.Join(tmpDir, "primary.json")}},
			{Name: "backup", Mode: "oauth", DefaultModel: "gpt-5.1-codex-mini", OAuth: config.UpstreamOAuthConfig{CredentialsFile: filepath.Join(tmpDir, "backup.json")}},
		},
	}

	var out bytes.Buffer
	var selectedPath string
	restore := setAuthLoginExecutorForTest(func(_ context.Context, cfg *config.Config, out io.Writer) error {
		selectedPath = cfg.OAuth.CredentialsFile
		_, _ = io.WriteString(out, "stub login\n")
		return nil
	})
	defer restore()

	err := AuthLogin(context.Background(), cfg, strings.NewReader("2\n"), &out, "")
	if err != nil {
		t.Fatalf("AuthLogin returned error: %v", err)
	}
	if selectedPath != filepath.Join(tmpDir, "backup.json") {
		t.Fatalf("expected backup credentials file, got %q", selectedPath)
	}
	if !strings.Contains(out.String(), "backup") {
		t.Fatalf("expected selector output to mention backup, got %q", out.String())
	}
}

func TestAuthLogin_ValidCredentialRequiresConfirmation(t *testing.T) {
	tmpDir := t.TempDir()
	validPath := filepath.Join(tmpDir, "primary.json")
	if err := oauth.NewStore(validPath).Save(&oauth.Credentials{
		AccessToken:  "at",
		RefreshToken: "rt",
		Email:        "valid@example.com",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatalf("save valid creds: %v", err)
	}

	cfg := &config.Config{
		OAuth: config.OAuthConfig{
			CallbackHost:    "localhost",
			CallbackPort:    0,
			CallbackPath:    "/auth/callback",
			AutoOpenBrowser: false,
		},
		Upstreams: []config.NamedUpstreamConfig{
			{Name: "primary", Mode: "oauth", DefaultModel: "gpt-5.1-codex-mini", OAuth: config.UpstreamOAuthConfig{CredentialsFile: validPath}},
		},
	}

	var out bytes.Buffer
	called := false
	restore := setAuthLoginExecutorForTest(func(_ context.Context, cfg *config.Config, out io.Writer) error {
		called = true
		return nil
	})
	defer restore()

	err := AuthLogin(context.Background(), cfg, strings.NewReader("n\n"), &out, "")
	if !errors.Is(err, errAuthLoginCanceled) {
		t.Fatalf("expected cancel error, got %v", err)
	}
	if called {
		t.Fatal("expected login executor not to be called")
	}
	if !strings.Contains(out.String(), "仍要重新登录覆盖") {
		t.Fatalf("expected overwrite confirmation prompt, got %q", out.String())
	}
}

package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openai-local-gateway/internal/config"
	"openai-local-gateway/internal/oauth"
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

	err := AuthLogin(ctx, cfg, &out)
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

	_ = AuthLogin(ctx, cfg, &out)

	output := out.String()
	if !strings.Contains(output, "redirect_uri=http%3A%2F%2Flocalhost%3A0%2Fauth%2Fcallback") {
		t.Fatalf("expected configured redirect_uri in authorization URL, got: %q", output)
	}
}

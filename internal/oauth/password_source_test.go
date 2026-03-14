package oauth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type passwordLoginExecutorStub struct {
	cred  *Credentials
	err   error
	calls int
}

func (s *passwordLoginExecutorStub) Login(context.Context, PasswordLoginRequest) (*Credentials, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.cred, nil
}

func TestPasswordTokenSource_UsesPasswordLoginWhenCredentialsMissing(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "oauth.json"))
	login := &passwordLoginExecutorStub{
		cred: &Credentials{
			AccessToken:  "at-password",
			RefreshToken: "rt-password",
			ExpiresAt:    time.Now().Add(time.Hour).Unix(),
			ClientID:     DefaultClientID,
			Email:        "user@example.com",
		},
	}
	source := NewPasswordTokenSource(store, &flowStub{}, login, PasswordLoginRequest{
		Email:       "user@example.com",
		Password:    "secret",
		ClientID:    DefaultClientID,
		RedirectURI: "http://localhost:1455/auth/callback",
	}, 30*time.Second)

	token, err := source.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken returned error: %v", err)
	}
	if token != "at-password" {
		t.Fatalf("unexpected token: %q", token)
	}
	if login.calls != 1 {
		t.Fatalf("expected one login call, got %d", login.calls)
	}
}

func TestPasswordTokenSource_RefreshesBeforePasswordLoginFallback(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "oauth.json"))
	if err := store.Save(&Credentials{
		AccessToken:  "expired",
		RefreshToken: "rt-existing",
		ExpiresAt:    time.Now().Add(-time.Minute).Unix(),
		ClientID:     DefaultClientID,
	}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	login := &passwordLoginExecutorStub{
		cred: &Credentials{
			AccessToken:  "at-password",
			RefreshToken: "rt-password",
			ExpiresAt:    time.Now().Add(time.Hour).Unix(),
			ClientID:     DefaultClientID,
		},
	}
	source := NewPasswordTokenSource(store, &flowStub{
		refreshFunc: func(ctx context.Context, refreshToken string, clientID string) (*Credentials, error) {
			return nil, errors.New("refresh failed")
		},
	}, login, PasswordLoginRequest{
		Email:       "user@example.com",
		Password:    "secret",
		ClientID:    DefaultClientID,
		RedirectURI: "http://localhost:1455/auth/callback",
	}, 30*time.Second)

	token, err := source.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken returned error: %v", err)
	}
	if token != "at-password" {
		t.Fatalf("unexpected token: %q", token)
	}
	if login.calls != 1 {
		t.Fatalf("expected password login fallback, got %d calls", login.calls)
	}
}

func TestPasswordTokenSource_UsesValidStoredCredentials(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "oauth.json"))
	if err := store.Save(&Credentials{
		AccessToken:  "at-valid",
		RefreshToken: "rt-valid",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		ClientID:     DefaultClientID,
	}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	login := &passwordLoginExecutorStub{}
	source := NewPasswordTokenSource(store, &flowStub{}, login, PasswordLoginRequest{
		Email:       "user@example.com",
		Password:    "secret",
		ClientID:    DefaultClientID,
		RedirectURI: "http://localhost:1455/auth/callback",
	}, 30*time.Second)

	token, err := source.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken returned error: %v", err)
	}
	if token != "at-valid" {
		t.Fatalf("unexpected token: %q", token)
	}
	if login.calls != 0 {
		t.Fatalf("expected no password login, got %d calls", login.calls)
	}
}

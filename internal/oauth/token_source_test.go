package oauth

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

type flowStub struct {
	refreshFunc func(ctx context.Context, refreshToken string, clientID string) (*Credentials, error)
}

func (f *flowStub) RefreshToken(ctx context.Context, refreshToken string, clientID string) (*Credentials, error) {
	return f.refreshFunc(ctx, refreshToken, clientID)
}

func TestTokenSource_AccessToken_RefreshesExpiredToken(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "oauth.json"))
	if err := store.Save(&Credentials{
		AccessToken:  "expired-at",
		RefreshToken: "rt-123",
		ExpiresAt:    time.Now().Add(-time.Minute).Unix(),
		ClientID:     DefaultClientID,
	}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	source := NewTokenSource(store, &flowStub{
		refreshFunc: func(ctx context.Context, refreshToken string, clientID string) (*Credentials, error) {
			return &Credentials{
				AccessToken:  "fresh-at",
				RefreshToken: "fresh-rt",
				ExpiresAt:    time.Now().Add(time.Hour).Unix(),
				ClientID:     clientID,
			}, nil
		},
	}, 30*time.Second)

	token, err := source.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken returned error: %v", err)
	}
	if token != "fresh-at" {
		t.Fatalf("unexpected token: %q", token)
	}

	updated, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if updated.AccessToken != "fresh-at" {
		t.Fatalf("store not updated: %#v", updated)
	}
}

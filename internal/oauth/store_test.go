package oauth

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStore_SaveAndLoadCredentials(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "openai-oauth.json"))
	cred := &Credentials{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}

	if err := store.Save(cred); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got.AccessToken != "at" {
		t.Fatalf("unexpected access token: %q", got.AccessToken)
	}
	if got.RefreshToken != "rt" {
		t.Fatalf("unexpected refresh token: %q", got.RefreshToken)
	}
}

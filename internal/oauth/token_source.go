package oauth

import (
	"context"
	"sync"
	"time"
)

type Refresher interface {
	RefreshToken(ctx context.Context, refreshToken string, clientID string) (*Credentials, error)
}

type TokenSource struct {
	store         *Store
	refresher     Refresher
	refreshLeeway time.Duration
	mu            sync.Mutex
}

func NewTokenSource(store *Store, refresher Refresher, refreshLeeway time.Duration) *TokenSource {
	if refreshLeeway <= 0 {
		refreshLeeway = 30 * time.Second
	}
	return &TokenSource{
		store:         store,
		refresher:     refresher,
		refreshLeeway: refreshLeeway,
	}
}

func (s *TokenSource) AccessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cred, err := s.store.Load()
	if err != nil {
		return "", err
	}
	if time.Unix(cred.ExpiresAt, 0).After(time.Now().Add(s.refreshLeeway)) {
		return cred.AccessToken, nil
	}
	refreshed, err := s.refresher.RefreshToken(ctx, cred.RefreshToken, cred.ClientID)
	if err != nil {
		return "", err
	}
	if refreshed.ClientID == "" {
		refreshed.ClientID = cred.ClientID
	}
	if err := s.store.Save(refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

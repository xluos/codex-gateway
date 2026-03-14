package oauth

import (
	"context"
	"time"
)

type PasswordLoginRequest struct {
	Email       string
	Password    string
	ClientID    string
	RedirectURI string
}

type PasswordLoginExecutor interface {
	Login(ctx context.Context, req PasswordLoginRequest) (*Credentials, error)
}

type PasswordTokenSource struct {
	store         *Store
	refresher     Refresher
	loginExecutor PasswordLoginExecutor
	request       PasswordLoginRequest
	refreshLeeway time.Duration
}

func NewPasswordTokenSource(store *Store, refresher Refresher, loginExecutor PasswordLoginExecutor, request PasswordLoginRequest, refreshLeeway time.Duration) *PasswordTokenSource {
	if refreshLeeway <= 0 {
		refreshLeeway = 30 * time.Second
	}
	return &PasswordTokenSource{
		store:         store,
		refresher:     refresher,
		loginExecutor: loginExecutor,
		request:       request,
		refreshLeeway: refreshLeeway,
	}
}

func (s *PasswordTokenSource) AccessToken(ctx context.Context) (string, error) {
	cred, err := s.store.Load()
	if err == nil {
		if time.Unix(cred.ExpiresAt, 0).After(time.Now().Add(s.refreshLeeway)) {
			return cred.AccessToken, nil
		}
		if cred.RefreshToken != "" && s.refresher != nil {
			refreshed, refreshErr := s.refresher.RefreshToken(ctx, cred.RefreshToken, firstNonEmptyString(cred.ClientID, s.request.ClientID))
			if refreshErr == nil {
				if refreshed.ClientID == "" {
					refreshed.ClientID = firstNonEmptyString(cred.ClientID, s.request.ClientID)
				}
				if refreshed.Email == "" {
					refreshed.Email = s.request.Email
				}
				if saveErr := s.store.Save(refreshed); saveErr == nil {
					return refreshed.AccessToken, nil
				}
			}
		}
	}

	cred, err = s.loginExecutor.Login(ctx, s.request)
	if err != nil {
		return "", err
	}
	if cred.ClientID == "" {
		cred.ClientID = s.request.ClientID
	}
	if cred.Email == "" {
		cred.Email = s.request.Email
	}
	if err := s.store.Save(cred); err != nil {
		return "", err
	}
	return cred.AccessToken, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

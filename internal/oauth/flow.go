package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	DefaultAuthorize     = "https://auth.openai.com/oauth/authorize"
	DefaultToken         = "https://auth.openai.com/oauth/token"
	DefaultRedirect      = "http://localhost:1455/auth/callback"
	DefaultScopes        = "openid profile email offline_access"
	DefaultRefreshScopes = "openid profile email"
)

type Config struct {
	AuthorizeURL string
	TokenURL     string
	RedirectURI  string
	ClientID     string
	Scopes       string
}

type AuthSession struct {
	State        string
	CodeVerifier string
	RedirectURI  string
	ClientID     string
}

type AuthURLResult struct {
	AuthURL      string
	State        string
	CodeVerifier string
	RedirectURI  string
	ClientID     string
}

type Flow struct {
	cfg        Config
	httpClient *http.Client
}

func NewFlow(cfg Config, httpClient *http.Client) *Flow {
	if strings.TrimSpace(cfg.AuthorizeURL) == "" {
		cfg.AuthorizeURL = DefaultAuthorize
	}
	if strings.TrimSpace(cfg.TokenURL) == "" {
		cfg.TokenURL = DefaultToken
	}
	if strings.TrimSpace(cfg.RedirectURI) == "" {
		cfg.RedirectURI = DefaultRedirect
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		cfg.ClientID = DefaultClientID
	}
	if strings.TrimSpace(cfg.Scopes) == "" {
		cfg.Scopes = DefaultScopes
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Flow{cfg: cfg, httpClient: httpClient}
}

func (f *Flow) GenerateAuthURL() (*AuthURLResult, error) {
	state, err := generateHex(32)
	if err != nil {
		return nil, err
	}
	verifier, err := generateHex(64)
	if err != nil {
		return nil, err
	}
	challenge := generateCodeChallenge(verifier)
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", f.cfg.ClientID)
	params.Set("redirect_uri", f.cfg.RedirectURI)
	params.Set("scope", f.cfg.Scopes)
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("id_token_add_organizations", "true")
	params.Set("codex_cli_simplified_flow", "true")

	return &AuthURLResult{
		AuthURL:      f.cfg.AuthorizeURL + "?" + params.Encode(),
		State:        state,
		CodeVerifier: verifier,
		RedirectURI:  f.cfg.RedirectURI,
		ClientID:     f.cfg.ClientID,
	}, nil
}

func (f *Flow) ExchangeCode(ctx context.Context, session *AuthURLResult, code, state string) (*Credentials, error) {
	if session == nil {
		return nil, errors.New("oauth session is required")
	}
	if strings.TrimSpace(state) == "" || strings.TrimSpace(state) != strings.TrimSpace(session.State) {
		return nil, errors.New("invalid oauth state")
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", session.ClientID)
	form.Set("code", code)
	form.Set("redirect_uri", session.RedirectURI)
	form.Set("code_verifier", session.CodeVerifier)
	return f.exchange(ctx, form, session.ClientID)
}

func (f *Flow) RefreshToken(ctx context.Context, refreshToken string, clientID string) (*Credentials, error) {
	if strings.TrimSpace(clientID) == "" {
		clientID = f.cfg.ClientID
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", clientID)
	form.Set("refresh_token", refreshToken)
	form.Set("scope", DefaultRefreshScopes)
	return f.exchange(ctx, form, clientID)
}

func (f *Flow) exchange(ctx context.Context, form url.Values, clientID string) (*Credentials, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codex-cli/0.91.0")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oauth token exchange failed with status %d", resp.StatusCode)
	}
	cred := &Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Unix(),
		ClientID:     clientID,
	}
	if userInfo, err := parseIDToken(tokenResp.IDToken); err == nil && userInfo != nil {
		cred.Email = userInfo.Email
		cred.ChatGPTAccountID = userInfo.ChatGPTAccountID
		cred.ChatGPTUserID = userInfo.ChatGPTUserID
		cred.OrganizationID = userInfo.OrganizationID
		cred.PlanType = userInfo.PlanType
	}
	return cred, nil
}

type userInfo struct {
	Email            string
	ChatGPTAccountID string
	ChatGPTUserID    string
	OrganizationID   string
	PlanType         string
}

func parseIDToken(idToken string) (*userInfo, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid id_token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims struct {
		Email string `json:"email"`
		Auth  struct {
			ChatGPTAccountID string `json:"chatgpt_account_id"`
			ChatGPTUserID    string `json:"chatgpt_user_id"`
			ChatGPTPlanType  string `json:"chatgpt_plan_type"`
			Organizations    []struct {
				ID        string `json:"id"`
				IsDefault bool   `json:"is_default"`
			} `json:"organizations"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	info := &userInfo{
		Email:            claims.Email,
		ChatGPTAccountID: claims.Auth.ChatGPTAccountID,
		ChatGPTUserID:    claims.Auth.ChatGPTUserID,
		PlanType:         claims.Auth.ChatGPTPlanType,
	}
	for _, org := range claims.Auth.Organizations {
		if org.IsDefault {
			info.OrganizationID = org.ID
			break
		}
	}
	if info.OrganizationID == "" && len(claims.Auth.Organizations) > 0 {
		info.OrganizationID = claims.Auth.Organizations[0].ID
	}
	return info, nil
}

func generateHex(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func generateCodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return strings.TrimRight(base64.URLEncoding.EncodeToString(sum[:]), "=")
}

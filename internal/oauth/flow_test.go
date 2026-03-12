package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testOAuthConfig(serverURL string) Config {
	return Config{
		AuthorizeURL: serverURL + "/oauth/authorize",
		TokenURL:     serverURL + "/oauth/token",
		RedirectURI:  "http://127.0.0.1:1455/auth/callback",
		ClientID:     DefaultClientID,
		Scopes:       DefaultScopes,
	}
}

func TestFlow_GenerateAuthURL_ReturnsStateAndVerifier(t *testing.T) {
	flow := NewFlow(testOAuthConfig("https://auth.openai.com"), http.DefaultClient)
	result, err := flow.GenerateAuthURL()
	if err != nil {
		t.Fatalf("GenerateAuthURL returned error: %v", err)
	}
	if result.State == "" || result.CodeVerifier == "" || result.AuthURL == "" {
		t.Fatal("missing oauth values")
	}
}

func TestFlow_ExchangeCode_SavesRefreshableCredentials(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Header.Get("User-Agent"); got != "codex-cli/0.91.0" {
			t.Fatalf("unexpected user-agent: %q", got)
		}
		if r.FormValue("grant_type") != "authorization_code" {
			t.Fatalf("unexpected grant_type: %s", r.FormValue("grant_type"))
		}
		if r.FormValue("code_verifier") == "" {
			t.Fatal("missing code_verifier")
		}
		resp := map[string]any{
			"access_token":  "at-123",
			"refresh_token": "rt-123",
			"expires_in":    3600,
			"id_token":      makeTestIDToken(time.Now().Add(time.Hour).Unix()),
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}))
	defer tokenServer.Close()

	flow := NewFlow(testOAuthConfig(tokenServer.URL), tokenServer.Client())
	session, err := flow.GenerateAuthURL()
	if err != nil {
		t.Fatalf("GenerateAuthURL returned error: %v", err)
	}

	cred, err := flow.ExchangeCode(context.Background(), session, "code-123", session.State)
	if err != nil {
		t.Fatalf("ExchangeCode returned error: %v", err)
	}
	if cred.AccessToken != "at-123" || cred.RefreshToken != "rt-123" {
		t.Fatalf("unexpected credentials: %#v", cred)
	}
	if cred.Email != "user@example.com" {
		t.Fatalf("unexpected email: %q", cred.Email)
	}
}

func TestFlow_RefreshToken_ReturnsUpdatedCredentials(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Header.Get("User-Agent"); got != "codex-cli/0.91.0" {
			t.Fatalf("unexpected user-agent: %q", got)
		}
		if r.FormValue("grant_type") != "refresh_token" {
			t.Fatalf("unexpected grant_type: %s", r.FormValue("grant_type"))
		}
		resp := map[string]any{
			"access_token":  "at-456",
			"refresh_token": "rt-456",
			"expires_in":    7200,
			"id_token":      makeTestIDToken(time.Now().Add(2 * time.Hour).Unix()),
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}))
	defer tokenServer.Close()

	flow := NewFlow(testOAuthConfig(tokenServer.URL), tokenServer.Client())
	cred, err := flow.RefreshToken(context.Background(), "rt-old", DefaultClientID)
	if err != nil {
		t.Fatalf("RefreshToken returned error: %v", err)
	}
	if cred.AccessToken != "at-456" {
		t.Fatalf("unexpected access token: %q", cred.AccessToken)
	}
}

func makeTestIDToken(exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadMap := map[string]any{
		"email": "user@example.com",
		"exp":   exp,
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acc_123",
			"chatgpt_user_id":    "user_123",
			"chatgpt_plan_type":  "plus",
			"user_id":            "user_123",
			"organizations": []map[string]any{
				{
					"id":         "org_123",
					"is_default": true,
					"role":       "owner",
					"title":      "Default",
				},
			},
		},
	}
	payloadBytes, _ := json.Marshal(payloadMap)
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return strings.Join([]string{header, payload, ""}, ".")
}

func TestFlow_GenerateAuthURL_ContainsExpectedQueryParams(t *testing.T) {
	flow := NewFlow(testOAuthConfig("https://auth.openai.com"), http.DefaultClient)
	result, err := flow.GenerateAuthURL()
	if err != nil {
		t.Fatalf("GenerateAuthURL returned error: %v", err)
	}

	parsed, err := url.Parse(result.AuthURL)
	if err != nil {
		t.Fatalf("Parse auth URL: %v", err)
	}
	query := parsed.Query()
	if query.Get("client_id") != DefaultClientID {
		t.Fatalf("unexpected client_id: %q", query.Get("client_id"))
	}
	if query.Get("code_challenge_method") != "S256" {
		t.Fatalf("unexpected challenge method: %q", query.Get("code_challenge_method"))
	}
}

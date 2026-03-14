package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHTTPPasswordLoginExecutor_Login(t *testing.T) {
	var gotEmailSentinel string
	var gotPasswordSentinel string

	sentinelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/sentinel/req" {
			t.Fatalf("unexpected sentinel path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode sentinel request: %v", err)
		}
		flow, _ := body["flow"].(string)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "challenge-" + flow,
			"proofofwork": map[string]any{
				"required":   true,
				"seed":       "seed-" + flow,
				"difficulty": "ffffffff",
			},
		})
	}))
	defer sentinelServer.Close()

	var authServer *httptest.Server
	authServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/authorize":
			http.SetCookie(w, &http.Cookie{Name: "login_session", Value: "sess", Path: "/"})
			http.Redirect(w, r, "/log-in", http.StatusFound)
		case "/log-in":
			w.WriteHeader(http.StatusOK)
		case "/api/accounts/authorize/continue":
			gotEmailSentinel = r.Header.Get("openai-sentinel-token")
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode continue body: %v", err)
			}
			username, _ := body["username"].(map[string]any)
			if username["value"] != "user@example.com" {
				t.Fatalf("unexpected username payload: %#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"page": map[string]any{"type": "password"},
			})
		case "/api/accounts/password/verify":
			gotPasswordSentinel = r.Header.Get("openai-sentinel-token")
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode password body: %v", err)
			}
			if body["password"] != "secret" {
				t.Fatalf("unexpected password payload: %#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"continue_url": authServer.URL + "/sign-in-with-chatgpt/codex/consent",
				"page":         map[string]any{"type": "consent"},
			})
		case "/sign-in-with-chatgpt/codex/consent":
			http.Redirect(w, r, "http://localhost:1455/auth/callback?code=auth-code-123&state=test-state", http.StatusFound)
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if r.Form.Get("code") != "auth-code-123" {
				t.Fatalf("unexpected authorization code: %q", r.Form.Get("code"))
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "at-123",
				"refresh_token": "rt-123",
				"id_token":      makeTestIDToken(time.Now().Add(time.Hour).Unix()),
				"expires_in":    3600,
			})
		default:
			t.Fatalf("unexpected auth path: %s", r.URL.Path)
		}
	}))
	defer authServer.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := authServer.Client()
	client.Jar = jar

	executor := &HTTPPasswordLoginExecutor{
		AuthBaseURL:     authServer.URL,
		SentinelBaseURL: sentinelServer.URL,
		HTTPClient:      client,
		StateGenerator: func() string {
			return "test-state"
		},
	}

	cred, err := executor.Login(context.Background(), PasswordLoginRequest{
		Email:       "user@example.com",
		Password:    "secret",
		ClientID:    DefaultClientID,
		RedirectURI: "http://localhost:1455/auth/callback",
	})
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if cred.AccessToken != "at-123" || cred.RefreshToken != "rt-123" {
		t.Fatalf("unexpected credentials: %#v", cred)
	}
	if !strings.Contains(gotEmailSentinel, `"flow":"authorize_continue"`) {
		t.Fatalf("expected authorize_continue sentinel token, got %q", gotEmailSentinel)
	}
	if !strings.Contains(gotPasswordSentinel, `"flow":"password_verify"`) {
		t.Fatalf("expected password_verify sentinel token, got %q", gotPasswordSentinel)
	}
}

func TestHTTPPasswordLoginExecutor_LoginSkipsAuthorizeRedirectChallenge(t *testing.T) {
	sentinelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "challenge",
			"proofofwork": map[string]any{
				"required":   true,
				"seed":       "seed",
				"difficulty": "ffffffff",
			},
		})
	}))
	defer sentinelServer.Close()

	var authServer *httptest.Server
	authServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/authorize":
			http.SetCookie(w, &http.Cookie{Name: "login_session", Value: "sess", Path: "/"})
			http.Redirect(w, r, "/api/oauth/oauth2/auth", http.StatusFound)
		case "/api/oauth/oauth2/auth":
			http.Error(w, "blocked", http.StatusForbidden)
		case "/api/accounts/authorize/continue":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"page": map[string]any{"type": "password"}})
		case "/api/accounts/password/verify":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"continue_url": authServer.URL + "/sign-in-with-chatgpt/codex/consent",
				"page":         map[string]any{"type": "consent"},
			})
		case "/sign-in-with-chatgpt/codex/consent":
			http.Redirect(w, r, "http://localhost:1455/auth/callback?code=auth-code-123&state=test-state", http.StatusFound)
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "at-123",
				"refresh_token": "rt-123",
				"id_token":      makeTestIDToken(time.Now().Add(time.Hour).Unix()),
				"expires_in":    3600,
			})
		default:
			t.Fatalf("unexpected auth path: %s", r.URL.Path)
		}
	}))
	defer authServer.Close()

	executor := &HTTPPasswordLoginExecutor{
		AuthBaseURL:     authServer.URL,
		SentinelBaseURL: sentinelServer.URL,
		HTTPClient:      authServer.Client(),
		StateGenerator: func() string {
			return "test-state"
		},
	}

	cred, err := executor.Login(context.Background(), PasswordLoginRequest{
		Email:       "user@example.com",
		Password:    "secret",
		ClientID:    DefaultClientID,
		RedirectURI: "http://localhost:1455/auth/callback",
	})
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if cred.AccessToken != "at-123" {
		t.Fatalf("unexpected access token: %q", cred.AccessToken)
	}
}

func TestHTTPPasswordLoginExecutor_LoginHandlesLargeSentinelChallenge(t *testing.T) {
	largeToken := strings.Repeat("x", 12000)
	sentinelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": largeToken,
			"proofofwork": map[string]any{
				"required":   true,
				"seed":       "seed",
				"difficulty": "ffffffff",
			},
		})
	}))
	defer sentinelServer.Close()

	var authServer *httptest.Server
	authServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/authorize":
			http.SetCookie(w, &http.Cookie{Name: "login_session", Value: "sess", Path: "/"})
			http.Redirect(w, r, "/api/oauth/oauth2/auth", http.StatusFound)
		case "/api/accounts/authorize/continue":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"page": map[string]any{"type": "password"}})
		case "/api/accounts/password/verify":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"continue_url": authServer.URL + "/sign-in-with-chatgpt/codex/consent",
				"page":         map[string]any{"type": "consent"},
			})
		case "/sign-in-with-chatgpt/codex/consent":
			http.Redirect(w, r, "http://localhost:1455/auth/callback?code=auth-code-123&state=test-state", http.StatusFound)
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "at-123",
				"refresh_token": "rt-123",
				"id_token":      makeTestIDToken(time.Now().Add(time.Hour).Unix()),
				"expires_in":    3600,
			})
		case "/api/oauth/oauth2/auth":
			http.Error(w, "blocked", http.StatusForbidden)
		default:
			t.Fatalf("unexpected auth path: %s", r.URL.Path)
		}
	}))
	defer authServer.Close()

	executor := &HTTPPasswordLoginExecutor{
		AuthBaseURL:     authServer.URL,
		SentinelBaseURL: sentinelServer.URL,
		HTTPClient:      authServer.Client(),
		StateGenerator: func() string {
			return "test-state"
		},
	}

	cred, err := executor.Login(context.Background(), PasswordLoginRequest{
		Email:       "user@example.com",
		Password:    "secret",
		ClientID:    DefaultClientID,
		RedirectURI: "http://localhost:1455/auth/callback",
	})
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if cred.AccessToken != "at-123" {
		t.Fatalf("unexpected access token: %q", cred.AccessToken)
	}
}

func TestHTTPPasswordLoginExecutor_LoginReturnsOTPError(t *testing.T) {
	sentinelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "challenge",
			"proofofwork": map[string]any{
				"required":   true,
				"seed":       "seed",
				"difficulty": "ffffffff",
			},
		})
	}))
	defer sentinelServer.Close()

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/authorize":
			http.SetCookie(w, &http.Cookie{Name: "login_session", Value: "sess", Path: "/"})
			w.WriteHeader(http.StatusOK)
		case "/api/accounts/authorize/continue":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"page": map[string]any{"type": "password"}})
		case "/api/accounts/password/verify":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"continue_url": "/email-verification",
				"page":         map[string]any{"type": "email_otp_verification"},
			})
		default:
			t.Fatalf("unexpected auth path: %s", r.URL.Path)
		}
	}))
	defer authServer.Close()

	executor := &HTTPPasswordLoginExecutor{
		AuthBaseURL:     authServer.URL,
		SentinelBaseURL: sentinelServer.URL,
		HTTPClient:      authServer.Client(),
	}

	_, err := executor.Login(context.Background(), PasswordLoginRequest{
		Email:       "user@example.com",
		Password:    "secret",
		ClientID:    DefaultClientID,
		RedirectURI: "http://localhost:1455/auth/callback",
	})
	if err == nil || !strings.Contains(err.Error(), "email otp verification requires an OTP provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildSentinelToken(t *testing.T) {
	tokenBuilder := SentinelTokenBuilder{
		DeviceID: "device-123",
		Now: func() time.Time {
			return time.Unix(1710000000, 0).UTC()
		},
		RandomFloat: func() float64 { return 0.25 },
		RandomIntn: func(int) int { return 0 },
	}

	token, err := tokenBuilder.BuildToken(SentinelChallenge{
		Token: "challenge-123",
		ProofOfWork: SentinelProofOfWork{
			Required:   true,
			Seed:       "seed-123",
			Difficulty: "ffffffff",
		},
	}, "authorize_continue")
	if err != nil {
		t.Fatalf("BuildToken returned error: %v", err)
	}
	var payload struct {
		P    string `json:"p"`
		T    string `json:"t"`
		C    string `json:"c"`
		ID   string `json:"id"`
		Flow string `json:"flow"`
	}
	if err := json.Unmarshal([]byte(token), &payload); err != nil {
		t.Fatalf("unmarshal token: %v", err)
	}
	if payload.C != "challenge-123" || payload.ID != "device-123" || payload.Flow != "authorize_continue" {
		t.Fatalf("unexpected sentinel payload: %#v", payload)
	}
	if !strings.HasPrefix(payload.P, "gAAAAAB") {
		t.Fatalf("expected proof token prefix, got %q", payload.P)
	}
}

func TestSentinelTokenBuilder_BuildRequirementsToken(t *testing.T) {
	tokenBuilder := SentinelTokenBuilder{
		DeviceID: "device-123",
		Now: func() time.Time {
			return time.Unix(1710000000, 0).UTC()
		},
		RandomFloat: func() float64 { return 0.25 },
		RandomIntn: func(int) int { return 0 },
	}

	token, err := tokenBuilder.BuildRequirementsToken()
	if err != nil {
		t.Fatalf("BuildRequirementsToken returned error: %v", err)
	}
	if !strings.HasPrefix(token, "gAAAAAC") {
		t.Fatalf("expected requirements token prefix, got %q", token)
	}
	raw := strings.TrimPrefix(token, "gAAAAAC")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode requirements token: %v", err)
	}
	var values []any
	if err := json.Unmarshal(decoded, &values); err != nil {
		t.Fatalf("unmarshal requirements payload: %v", err)
	}
	if len(values) != 19 {
		t.Fatalf("unexpected config length: %d", len(values))
	}
}

func TestDecodeAuthSessionCookie(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"workspaces":[{"id":"ws_123"}]}`))
	cookies := []*http.Cookie{{Name: "oai-client-auth-session", Value: payload + ".ts.sig"}}

	sessionData, err := decodeAuthSessionCookie(cookies)
	if err != nil {
		t.Fatalf("decodeAuthSessionCookie returned error: %v", err)
	}
	workspaces, ok := sessionData["workspaces"].([]any)
	if !ok || len(workspaces) != 1 {
		t.Fatalf("unexpected workspaces payload: %#v", sessionData)
	}
}

func TestExtractAuthorizationCode(t *testing.T) {
	code, err := extractAuthorizationCode("http://localhost:1455/auth/callback?code=abc123&state=test")
	if err != nil {
		t.Fatalf("extractAuthorizationCode returned error: %v", err)
	}
	if code != "abc123" {
		t.Fatalf("unexpected code: %q", code)
	}

	_, err = extractAuthorizationCode("http://localhost:1455/auth/callback?state=test")
	if err == nil {
		t.Fatal("expected missing code error")
	}
}

func TestJoinURLPath(t *testing.T) {
	baseURL, _ := url.Parse("https://auth.openai.com")
	got := joinURLPath(baseURL, "/oauth/authorize")
	if got != "https://auth.openai.com/oauth/authorize" {
		t.Fatalf("unexpected URL: %q", got)
	}
}

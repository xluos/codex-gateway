package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCodexPasswordFlow_LoginOTPBranch(t *testing.T) {
	var validateAttempts []string

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
				"continue_url": "/email-verification",
				"page":         map[string]any{"type": "email_otp_verification"},
			})
		case "/api/accounts/email-otp/validate":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode otp validate body: %v", err)
			}
			validateAttempts = append(validateAttempts, body["code"].(string))
			if body["code"] == "654321" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"continue_url": authServer.URL + "/sign-in-with-chatgpt/codex/consent",
					"page":         map[string]any{"type": "consent"},
				})
				return
			}
			http.Error(w, "bad code", http.StatusBadRequest)
		case "/sign-in-with-chatgpt/codex/consent":
			http.Redirect(w, r, "http://localhost:1455/auth/callback?code=otp-code-123&state=test-state", http.StatusFound)
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "at-otp",
				"refresh_token": "rt-otp",
				"id_token":      makeTestIDToken(time.Now().Add(time.Hour).Unix()),
				"expires_in":    3600,
			})
		default:
			t.Fatalf("unexpected auth path: %s", r.URL.Path)
		}
	}))
	defer authServer.Close()

	flow := &CodexPasswordFlow{
		AuthBaseURL:     authServer.URL,
		SentinelBaseURL: sentinelServer.URL,
		OTPProvider: OTPProviderFunc(func(ctx context.Context, email string) ([]string, error) {
			return []string{"111111", "654321"}, nil
		}),
	}

	cred, err := flow.Login(context.Background(), PasswordLoginRequest{
		Email:       "user@example.com",
		Password:    "secret",
		ClientID:    DefaultClientID,
		RedirectURI: "http://localhost:1455/auth/callback",
	})
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if cred.AccessToken != "at-otp" {
		t.Fatalf("unexpected access token: %q", cred.AccessToken)
	}
	if got := strings.Join(validateAttempts, ","); got != "111111,654321" {
		t.Fatalf("unexpected OTP attempts: %s", got)
	}
}

func TestCodexPasswordFlow_LoginAboutYouAndOrganizationBranch(t *testing.T) {
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

	var createAccountCalled bool
	var workspaceSelectCalled bool
	var organizationSelectCalled bool

	authSessionPayload := `{"workspaces":[{"id":"ws_123","kind":"personal"}]}`

	var authServer *httptest.Server
	authServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/authorize":
			http.SetCookie(w, &http.Cookie{Name: "login_session", Value: "sess", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "oai-client-auth-session", Value: encodeAuthSessionCookie(authSessionPayload), Path: "/"})
			http.Redirect(w, r, "/api/oauth/oauth2/auth", http.StatusFound)
		case "/api/oauth/oauth2/auth":
			http.Error(w, "blocked", http.StatusForbidden)
		case "/api/accounts/authorize/continue":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"page": map[string]any{"type": "password"}})
		case "/api/accounts/password/verify":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"continue_url": "/about-you",
				"page":         map[string]any{"type": "email_otp_verification"},
			})
		case "/api/accounts/email-otp/validate":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"continue_url": "/about-you",
				"page":         map[string]any{"type": "about_you"},
			})
		case "/about-you":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("about-you"))
		case "/api/accounts/create_account":
			createAccountCalled = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"continue_url": authServer.URL + "/sign-in-with-chatgpt/codex/consent",
			})
		case "/sign-in-with-chatgpt/codex/consent":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("consent"))
		case "/api/accounts/workspace/select":
			workspaceSelectCalled = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"continue_url": "/organization",
				"page":         map[string]any{"type": "organization"},
				"data": map[string]any{
					"orgs": []map[string]any{
						{
							"id": "org_123",
							"projects": []map[string]any{
								{"id": "proj_123"},
							},
						},
					},
				},
			})
		case "/api/accounts/organization/select":
			organizationSelectCalled = true
			http.Redirect(w, r, "http://localhost:1455/auth/callback?code=org-code-123&state=test-state", http.StatusFound)
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "at-org",
				"refresh_token": "rt-org",
				"id_token":      makeTestIDToken(time.Now().Add(time.Hour).Unix()),
				"expires_in":    3600,
			})
		default:
			t.Fatalf("unexpected auth path: %s", r.URL.Path)
		}
	}))
	defer authServer.Close()

	flow := &CodexPasswordFlow{
		AuthBaseURL:     authServer.URL,
		SentinelBaseURL: sentinelServer.URL,
		OTPProvider: OTPProviderFunc(func(ctx context.Context, email string) ([]string, error) {
			return []string{"654321"}, nil
		}),
	}

	cred, err := flow.Login(context.Background(), PasswordLoginRequest{
		Email:       "user@example.com",
		Password:    "secret",
		ClientID:    DefaultClientID,
		RedirectURI: "http://localhost:1455/auth/callback",
	})
	if err != nil {
		t.Fatalf("Login returned error: %v (create=%v workspace=%v org=%v)", err, createAccountCalled, workspaceSelectCalled, organizationSelectCalled)
	}
	if cred.AccessToken != "at-org" {
		t.Fatalf("unexpected access token: %q", cred.AccessToken)
	}
	if !createAccountCalled || !workspaceSelectCalled || !organizationSelectCalled {
		t.Fatalf("expected about-you and org branches to run, got create=%v workspace=%v org=%v", createAccountCalled, workspaceSelectCalled, organizationSelectCalled)
	}
}

type OTPProviderFunc func(ctx context.Context, email string) ([]string, error)

func (f OTPProviderFunc) Codes(ctx context.Context, email string) ([]string, error) {
	return f(ctx, email)
}

func encodeAuthSessionCookie(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".ts.sig"
}

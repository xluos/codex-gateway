package upstream

import (
	"context"
	"net/http"
	"testing"
	"time"
)

type tokenSourceStub struct {
	token string
}

func (s tokenSourceStub) AccessToken(context.Context) (string, error) {
	return s.token, nil
}

func TestNewRequest_InjectsUpstreamAuthorization(t *testing.T) {
	client := NewClient("https://api.openai.com", "sk-upstream", time.Minute)
	req, err := client.NewRequest(context.Background(), http.MethodGet, "/v1/models", nil, nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-upstream" {
		t.Fatalf("unexpected authorization header: %q", got)
	}
	if got := req.URL.String(); got != "https://api.openai.com/v1/models" {
		t.Fatalf("unexpected url: %q", got)
	}
}

func TestNewRequest_UsesOAuthAccessToken(t *testing.T) {
	client := NewOAuthClient("https://api.openai.com", tokenSourceStub{token: "oauth-at"}, time.Minute)
	req, err := client.NewRequest(context.Background(), http.MethodGet, "/v1/models", nil, nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer oauth-at" {
		t.Fatalf("unexpected authorization header: %q", got)
	}
}

func TestNewOAuthClient_CodexResponsesURLUsesChatGPTHostForOpenAIAPIBase(t *testing.T) {
	client := NewOAuthClient("https://api.openai.com", tokenSourceStub{token: "oauth-at"}, time.Minute)
	if got := client.CodexResponsesURL(); got != "https://chatgpt.com/backend-api/codex/responses" {
		t.Fatalf("unexpected codex responses url: %q", got)
	}
}

func TestNewOAuthClient_CodexResponsesURLUsesCustomBaseForNonOpenAIHosts(t *testing.T) {
	client := NewOAuthClient("http://127.0.0.1:18080", tokenSourceStub{token: "oauth-at"}, time.Minute)
	if got := client.CodexResponsesURL(); got != "http://127.0.0.1:18080/backend-api/codex/responses" {
		t.Fatalf("unexpected codex responses url: %q", got)
	}
}

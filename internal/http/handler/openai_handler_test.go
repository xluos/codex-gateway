package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"codex-gateway/internal/oauth"
	"codex-gateway/internal/upstream"
)

func newTestHandler(t *testing.T, upstreamServer *httptest.Server) *OpenAIHandler {
	t.Helper()
	return NewOpenAIHandler(upstream.NewClient(upstreamServer.URL, "sk-upstream", time.Minute))
}

type testTokenSource struct {
	token string
	err   error
}

func (s testTokenSource) AccessToken(context.Context) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.token, nil
}

type testCredentialsLoader struct {
	cred *oauth.Credentials
	err  error
}

func (s testCredentialsLoader) Load() (*oauth.Credentials, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.cred, nil
}

type testLogSink struct {
	mu    sync.Mutex
	lines []string
}

func (s *testLogSink) Printf(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, fmt.Sprintf(format, args...))
}

func (s *testLogSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.lines, "\n")
}

func TestResponsesHandler_RejectsMissingModel(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called")
	}))
	defer upstreamServer.Close()

	h := newTestHandler(t, upstreamServer)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	h.Responses(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestChatCompletionsHandler_ProxiesValidRequest(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != `{"model":"gpt-4.1","stream":false}` {
			t.Fatalf("unexpected body: %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1"}`))
	}))
	defer upstreamServer.Close()

	h := newTestHandler(t, upstreamServer)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4.1","stream":false}`))
	w := httptest.NewRecorder()

	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != `{"id":"chatcmpl_1"}` {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestModelsHandler_ProxiesRequest(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstreamServer.Close()

	h := newTestHandler(t, upstreamServer)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	h.Models(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != `{"data":[]}` {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestModelsHandler_OAuthModeReturnsLocalModelList(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("oauth models should not call upstream")
	}))
	defer upstreamServer.Close()

	h := NewOpenAIHandler(upstream.NewOAuthClient(upstreamServer.URL, testTokenSource{token: "oauth-at"}, time.Minute))
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	h.Models(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(payload.Data) == 0 {
		t.Fatal("expected local model list")
	}
	if payload.Data[0]["id"] == nil {
		t.Fatalf("expected model id in response: %s", w.Body.String())
	}
}

func TestResponsesHandler_OAuthModeUsesCodexEndpointAndTransformsRequest(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/codex/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-at" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-ID"); got != "chatgpt-acc" {
			t.Fatalf("unexpected chatgpt-account-id header: %q", got)
		}
		if got := r.Header.Get("OpenAI-Beta"); got != "responses=experimental" {
			t.Fatalf("unexpected OpenAI-Beta header: %q", got)
		}
		if got := r.Header.Get("Originator"); got != "codex_cli_rs" {
			t.Fatalf("unexpected originator header: %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "codex_cli_rs/0.104.0" {
			t.Fatalf("unexpected user-agent header: %q", got)
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Fatalf("unexpected accept header: %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		if got, _ := payload["store"].(bool); got {
			t.Fatalf("expected store=false, got true")
		}
		if got, _ := payload["stream"].(bool); !got {
			t.Fatalf("expected stream=true, got false")
		}
		if instructions, _ := payload["instructions"].(string); strings.TrimSpace(instructions) == "" {
			t.Fatalf("expected default instructions, got %#v", payload["instructions"])
		}
		input, ok := payload["input"].([]any)
		if !ok || len(input) != 1 {
			t.Fatalf("expected normalized input array, got %#v", payload["input"])
		}
		first, ok := input[0].(map[string]any)
		if !ok {
			t.Fatalf("expected first input item to be object, got %#v", input[0])
		}
		if first["type"] != "message" || first["role"] != "user" {
			t.Fatalf("unexpected first input item: %#v", first)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\"}\n\ndata: [DONE]\n\n"))
	}))
	defer upstreamServer.Close()

	h := NewOpenAIHandler(
		upstream.NewOAuthClient(upstreamServer.URL, testTokenSource{token: "oauth-at"}, time.Minute),
		WithCredentialsLoader(testCredentialsLoader{cred: &oauth.Credentials{ChatGPTAccountID: "chatgpt-acc"}}),
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.1-codex","input":"Reply with ok."}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Responses(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("unexpected content type: %q", got)
	}
	if body := w.Body.String(); body != "data: {\"type\":\"response.created\"}\n\ndata: [DONE]\n\n" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestResponsesHandler_LogsUpstreamFailureDetails(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "req_123")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream bad request"}}`))
	}))
	defer upstreamServer.Close()

	logSink := &testLogSink{}
	h := NewOpenAIHandler(
		upstream.NewOAuthClient(upstreamServer.URL, testTokenSource{token: "oauth-at"}, time.Minute),
		WithCredentialsLoader(testCredentialsLoader{cred: &oauth.Credentials{ChatGPTAccountID: "chatgpt-acc"}}),
		WithLogger(logSink),
		WithDebugDumpHTTP(true),
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.1-codex","input":"Reply with ok."}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Responses(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected upstream status 400, got %d", w.Code)
	}
	logs := logSink.String()
	for _, want := range []string{
		"path=/v1/responses",
		"upstream=/backend-api/codex/responses",
		"status=400",
		"request_id=req_123",
		"upstream bad request",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("expected logs to contain %q, got %s", want, logs)
		}
	}
}

func TestChatCompletionsHandler_OAuthModeReturnsChatCompletionsJSON(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/codex/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`event: response.created`,
			`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.1-codex","status":"in_progress"}}`,
			``,
			`event: response.output_text.delta`,
			`data: {"type":"response.output_text.delta","delta":"OK"}`,
			``,
			`event: response.completed`,
			`data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.1-codex","status":"completed","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}]}}`,
			``,
		}, "\n")))
	}))
	defer upstreamServer.Close()

	h := NewOpenAIHandler(
		upstream.NewOAuthClient(upstreamServer.URL, testTokenSource{token: "oauth-at"}, time.Minute),
		WithCredentialsLoader(testCredentialsLoader{cred: &oauth.Credentials{ChatGPTAccountID: "chatgpt-acc"}}),
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.1-codex","stream":false,"messages":[{"role":"user","content":"Reply with exactly OK."}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var payload struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal chat completion: %v", err)
	}
	if payload.Object != "chat.completion" {
		t.Fatalf("unexpected object: %q", payload.Object)
	}
	if len(payload.Choices) != 1 || payload.Choices[0].Message.Content != "OK" {
		t.Fatalf("unexpected choices: %s", w.Body.String())
	}
	if payload.Usage.TotalTokens != 12 {
		t.Fatalf("unexpected usage: %#v", payload.Usage)
	}
}

func TestChatCompletionsHandler_OAuthModeStreamsChatChunks(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`event: response.created`,
			`data: {"type":"response.created","response":{"id":"resp_stream","model":"gpt-5.1-codex","status":"in_progress"}}`,
			``,
			`event: response.output_text.delta`,
			`data: {"type":"response.output_text.delta","delta":"OK"}`,
			``,
			`event: response.completed`,
			`data: {"type":"response.completed","response":{"id":"resp_stream","model":"gpt-5.1-codex","status":"completed","usage":{"input_tokens":8,"output_tokens":2,"total_tokens":10},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}]}}`,
			``,
		}, "\n")))
	}))
	defer upstreamServer.Close()

	h := NewOpenAIHandler(
		upstream.NewOAuthClient(upstreamServer.URL, testTokenSource{token: "oauth-at"}, time.Minute),
		WithCredentialsLoader(testCredentialsLoader{cred: &oauth.Credentials{ChatGPTAccountID: "chatgpt-acc"}}),
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.1-codex","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"Reply with exactly OK."}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("unexpected content type: %q", got)
	}
	body := w.Body.String()
	for _, want := range []string{
		`"object":"chat.completion.chunk"`,
		`"content":"OK"`,
		`"finish_reason":"stop"`,
		`data: [DONE]`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected streamed body to contain %q, got %s", want, body)
		}
	}
}

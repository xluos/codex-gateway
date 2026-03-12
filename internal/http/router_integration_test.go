package httpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codex-gateway/internal/http/handler"
	"codex-gateway/internal/upstream"
)

func TestEndToEnd_ModelsChatResponses(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1"}]}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"chatcmpl_1"}`))
		case "/v1/responses":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp_1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstreamServer.Close()

	openaiHandler := handler.NewOpenAIHandler(upstream.NewClient(upstreamServer.URL, "sk-upstream", time.Minute))
	router := NewRouter(testConfig(), openaiHandler)

	testCases := []struct {
		method string
		path   string
		body   []byte
		want   string
	}{
		{method: http.MethodGet, path: "/v1/models", want: `{"data":[{"id":"gpt-4.1"}]}`},
		{method: http.MethodPost, path: "/v1/chat/completions", body: []byte(`{"model":"gpt-4.1","stream":false}`), want: `{"id":"chatcmpl_1"}`},
		{method: http.MethodPost, path: "/v1/responses", body: []byte(`{"model":"gpt-4.1"}`), want: `{"id":"resp_1"}`},
	}

	for _, tc := range testCases {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer local-key")
		if len(tc.body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("path=%s expected 200, got %d", tc.path, w.Code)
		}
		if got := w.Body.String(); got != tc.want {
			t.Fatalf("path=%s unexpected body: %q", tc.path, got)
		}
	}
}

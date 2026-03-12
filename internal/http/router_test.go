package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"codex-gateway/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Host: "127.0.0.1",
			Port: 8081,
		},
		Auth: config.AuthConfig{
			APIKeys: []string{"local-key"},
		},
		Upstream: config.UpstreamConfig{
			BaseURL: "https://api.openai.com",
			APIKey:  "sk-upstream",
		},
		Compat: config.CompatConfig{
			EnableAliasRoutes: true,
		},
	}
}

func TestNewRouter_RegistersHealthz(t *testing.T) {
	r := NewRouter(testConfig(), nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRouter_RegistersAliasRoutes(t *testing.T) {
	r := NewRouter(testConfig(), nil)
	routes := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/models"},
		{method: http.MethodPost, path: "/chat/completions"},
		{method: http.MethodPost, path: "/responses"},
	}

	for _, route := range routes {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Header.Set("Authorization", "Bearer local-key")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Fatalf("path %s not registered", route.path)
		}
	}
}

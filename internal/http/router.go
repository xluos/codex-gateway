package httpserver

import (
	"net/http"

	"codex-gateway/internal/config"
	"codex-gateway/internal/http/middleware"
)

type OpenAIHandler interface {
	Models(http.ResponseWriter, *http.Request)
	ChatCompletions(http.ResponseWriter, *http.Request)
	Responses(http.ResponseWriter, *http.Request)
}

func NewRouter(cfg *config.Config, handler OpenAIHandler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	if cfg == nil {
		return mux
	}

	auth := middleware.RequireAPIKey(cfg.Auth.APIKeys)
	modelsHandler := auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handler == nil {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		handler.Models(w, r)
	}))
	chatHandler := auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handler == nil {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		handler.ChatCompletions(w, r)
	}))
	responsesHandler := auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handler == nil {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		handler.Responses(w, r)
	}))

	mux.Handle("/v1/models", modelsHandler)
	mux.Handle("/v1/chat/completions", chatHandler)
	mux.Handle("/v1/responses", responsesHandler)

	if cfg.Compat.EnableAliasRoutes {
		mux.Handle("/models", modelsHandler)
		mux.Handle("/chat/completions", chatHandler)
		mux.Handle("/responses", responsesHandler)
	}

	return mux
}

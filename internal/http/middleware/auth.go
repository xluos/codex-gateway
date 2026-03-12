package middleware

import (
	"encoding/json"
	"net/http"
	"strings"
)

func RequireAPIKey(keys []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		allowed[key] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const prefix = "Bearer "
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, prefix) {
				writeOpenAIError(w, http.StatusUnauthorized, "authentication_error", "Invalid API key")
				return
			}

			token := strings.TrimSpace(strings.TrimPrefix(authHeader, prefix))
			if _, ok := allowed[token]; !ok {
				writeOpenAIError(w, http.StatusUnauthorized, "authentication_error", "Invalid API key")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeOpenAIError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"type":    errType,
			"message": message,
		},
	})
}

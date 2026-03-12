package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"openai-local-gateway/internal/openai"
	"openai-local-gateway/internal/oauth"
	"openai-local-gateway/internal/upstream"
)

type Logger interface {
	Printf(format string, args ...any)
}

type CredentialsLoader interface {
	Load() (*oauth.Credentials, error)
}

type OpenAIHandler struct {
	client            *upstream.Client
	logger            Logger
	debugDumpHTTP     bool
	credentialsLoader CredentialsLoader
}

type Option func(*OpenAIHandler)

func NewOpenAIHandler(client *upstream.Client, opts ...Option) *OpenAIHandler {
	h := &OpenAIHandler{
		client: client,
		logger: noopLogger{},
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func WithLogger(logger Logger) Option {
	return func(h *OpenAIHandler) {
		if logger != nil {
			h.logger = logger
		}
	}
}

func WithDebugDumpHTTP(enabled bool) Option {
	return func(h *OpenAIHandler) {
		h.debugDumpHTTP = enabled
	}
}

func WithCredentialsLoader(loader CredentialsLoader) Option {
	return func(h *OpenAIHandler) {
		h.credentialsLoader = loader
	}
}

type noopLogger struct{}

func (noopLogger) Printf(string, ...any) {}

type requestLogFields struct {
	method        string
	path          string
	mode          string
	upstreamPath  string
	status        int
	duration      time.Duration
	requestID     string
	errorDetail   string
	requestBody   string
	responseBody  string
	localResponse bool
}

func (h *OpenAIHandler) Models(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if h.client != nil && h.client.Mode() == upstream.ModeOAuth {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(openai.DefaultModelsResponse())
		h.logRequest(requestLogFields{
			method:        r.Method,
			path:          r.URL.Path,
			mode:          upstream.ModeOAuth,
			upstreamPath:  "local-model-list",
			status:        http.StatusOK,
			duration:      time.Since(start),
			localResponse: true,
		})
		return
	}

	result, err := upstream.Proxy(w, r, h.client, "/v1/models")
	if err != nil {
		h.logRequest(requestLogFields{
			method:      r.Method,
			path:        r.URL.Path,
			mode:        h.mode(),
			upstreamPath: "/v1/models",
			status:      http.StatusBadGateway,
			duration:    time.Since(start),
			errorDetail: err.Error(),
		})
		writeOpenAIError(w, http.StatusBadGateway, "api_error", "Upstream request failed")
		return
	}
	h.logRequest(requestLogFields{
		method:       r.Method,
		path:         r.URL.Path,
		mode:         h.mode(),
		upstreamPath: result.UpstreamURL,
		status:       result.StatusCode,
		duration:     time.Since(start),
		requestID:    result.RequestID,
		responseBody: result.ErrorBodySnippet,
	})
}

func (h *OpenAIHandler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	body, err := validateAndRestoreRequestBody(r, openai.ValidateJSONRequest)
	if err != nil {
		h.logRequest(requestLogFields{
			method:      r.Method,
			path:        r.URL.Path,
			mode:        h.mode(),
			status:      http.StatusBadRequest,
			duration:    time.Since(start),
			errorDetail: err.Error(),
		})
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if h.client != nil && h.client.Mode() == upstream.ModeOAuth {
		h.proxyOAuthChatCompletions(w, r, body, start)
		return
	}
	r.Body = ioNopCloser(body)
	result, err := upstream.Proxy(w, r, h.client, "/v1/chat/completions")
	if err != nil {
		h.logRequest(requestLogFields{
			method:      r.Method,
			path:        r.URL.Path,
			mode:        h.mode(),
			upstreamPath: "/v1/chat/completions",
			status:      http.StatusBadGateway,
			duration:    time.Since(start),
			errorDetail: err.Error(),
		})
		writeOpenAIError(w, http.StatusBadGateway, "api_error", "Upstream request failed")
		return
	}
	h.logRequest(requestLogFields{
		method:       r.Method,
		path:         r.URL.Path,
		mode:         h.mode(),
		upstreamPath: result.UpstreamURL,
		status:       result.StatusCode,
		duration:     time.Since(start),
		requestID:    result.RequestID,
		responseBody: result.ErrorBodySnippet,
	})
}

func (h *OpenAIHandler) Responses(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	body, err := validateAndRestoreRequestBody(r, openai.ValidateResponsesRequest)
	if err != nil {
		h.logRequest(requestLogFields{
			method:      r.Method,
			path:        r.URL.Path,
			mode:        h.mode(),
			status:      http.StatusBadRequest,
			duration:    time.Since(start),
			errorDetail: err.Error(),
		})
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	if h.client != nil && h.client.Mode() == upstream.ModeOAuth {
		h.proxyOAuthResponses(w, r, body, start)
		return
	}

	r.Body = ioNopCloser(body)
	result, err := upstream.Proxy(w, r, h.client, "/v1/responses")
	if err != nil {
		h.logRequest(requestLogFields{
			method:      r.Method,
			path:        r.URL.Path,
			mode:        h.mode(),
			upstreamPath: "/v1/responses",
			status:      http.StatusBadGateway,
			duration:    time.Since(start),
			errorDetail: err.Error(),
		})
		writeOpenAIError(w, http.StatusBadGateway, "api_error", "Upstream request failed")
		return
	}
	h.logRequest(requestLogFields{
		method:       r.Method,
		path:         r.URL.Path,
		mode:         h.mode(),
		upstreamPath: result.UpstreamURL,
		status:       result.StatusCode,
		duration:     time.Since(start),
		requestID:    result.RequestID,
		responseBody: result.ErrorBodySnippet,
	})
}

func validateAndRestoreRequestBody(r *http.Request, validate func([]byte) error) ([]byte, error) {
	body, err := ioReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body")
	}
	if err := validate(body); err != nil {
		return nil, err
	}
	return body, nil
}

func ioReadAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func ioNopCloser(body []byte) io.ReadCloser {
	return io.NopCloser(bytes.NewReader(body))
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

func (h *OpenAIHandler) proxyOAuthResponses(w http.ResponseWriter, r *http.Request, body []byte, start time.Time) {
	normalizedBody, err := openai.NormalizeCodexResponsesRequest(body)
	if err != nil {
		h.logRequest(requestLogFields{
			method:      r.Method,
			path:        r.URL.Path,
			mode:        upstream.ModeOAuth,
			status:      http.StatusBadRequest,
			duration:    time.Since(start),
			errorDetail: fmt.Sprintf("normalize oauth request: %v", err),
		})
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "Failed to normalize OAuth request body")
		return
	}
	req, resp, err := h.doOAuthCodexRequest(r, normalizedBody)
	if err != nil {
		h.logRequest(requestLogFields{
			method:       r.Method,
			path:         r.URL.Path,
			mode:         upstream.ModeOAuth,
			upstreamPath: openai.CodexResponsesPath,
			status:       http.StatusBadGateway,
			duration:     time.Since(start),
			errorDetail:  err.Error(),
			requestBody:  logBody(h.debugDumpHTTP, normalizedBody),
		})
		writeOpenAIError(w, http.StatusBadGateway, "api_error", "Upstream request failed")
		return
	}
	defer resp.Body.Close()

	result, err := upstream.WriteResponse(w, resp, req.URL.Path)
	if err != nil {
		h.logRequest(requestLogFields{
			method:       r.Method,
			path:         r.URL.Path,
			mode:         upstream.ModeOAuth,
			upstreamPath: openai.CodexResponsesPath,
			status:       http.StatusBadGateway,
			duration:     time.Since(start),
			errorDetail:  fmt.Sprintf("write oauth upstream response: %v", err),
			requestBody:  logBody(h.debugDumpHTTP, normalizedBody),
		})
		writeOpenAIError(w, http.StatusBadGateway, "api_error", "Failed to write upstream response")
		return
	}

	h.logRequest(requestLogFields{
		method:       r.Method,
		path:         r.URL.Path,
		mode:         upstream.ModeOAuth,
		upstreamPath: result.UpstreamURL,
		status:       result.StatusCode,
		duration:     time.Since(start),
		requestID:    result.RequestID,
		requestBody:  logBody(h.debugDumpHTTP, normalizedBody),
		responseBody: logBody(h.debugDumpHTTP || result.StatusCode >= http.StatusBadRequest, []byte(result.ErrorBodySnippet)),
	})
}

func (h *OpenAIHandler) proxyOAuthChatCompletions(w http.ResponseWriter, r *http.Request, body []byte, start time.Time) {
	normalizedBody, meta, err := openai.ConvertChatCompletionsToResponses(body)
	if err != nil {
		h.logRequest(requestLogFields{
			method:      r.Method,
			path:        r.URL.Path,
			mode:        upstream.ModeOAuth,
			status:      http.StatusBadRequest,
			duration:    time.Since(start),
			errorDetail: err.Error(),
		})
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	req, resp, err := h.doOAuthCodexRequest(r, normalizedBody)
	if err != nil {
		h.logRequest(requestLogFields{
			method:       r.Method,
			path:         r.URL.Path,
			mode:         upstream.ModeOAuth,
			upstreamPath: openai.CodexResponsesPath,
			status:       http.StatusBadGateway,
			duration:     time.Since(start),
			errorDetail:  err.Error(),
			requestBody:  logBody(h.debugDumpHTTP, normalizedBody),
		})
		writeOpenAIError(w, http.StatusBadGateway, "api_error", "Upstream request failed")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		result, writeErr := upstream.WriteResponse(w, resp, req.URL.Path)
		if writeErr != nil {
			h.logRequest(requestLogFields{
				method:       r.Method,
				path:         r.URL.Path,
				mode:         upstream.ModeOAuth,
				upstreamPath: openai.CodexResponsesPath,
				status:       http.StatusBadGateway,
				duration:     time.Since(start),
				errorDetail:  fmt.Sprintf("write oauth upstream response: %v", writeErr),
				requestBody:  logBody(h.debugDumpHTTP, normalizedBody),
			})
			writeOpenAIError(w, http.StatusBadGateway, "api_error", "Failed to write upstream response")
			return
		}
		h.logRequest(requestLogFields{
			method:       r.Method,
			path:         r.URL.Path,
			mode:         upstream.ModeOAuth,
			upstreamPath: result.UpstreamURL,
			status:       result.StatusCode,
			duration:     time.Since(start),
			requestID:    result.RequestID,
			requestBody:  logBody(h.debugDumpHTTP, normalizedBody),
			responseBody: logBody(true, []byte(result.ErrorBodySnippet)),
		})
		return
	}

	var handleErr error
	if meta.ClientStream {
		handleErr = openai.WriteChatCompletionsStream(w, resp.Body, meta.OriginalModel, meta.IncludeUsage)
	} else {
		handleErr = openai.WriteChatCompletionsJSON(w, resp.Body, meta.OriginalModel)
	}
	if handleErr != nil {
		h.logRequest(requestLogFields{
			method:       r.Method,
			path:         r.URL.Path,
			mode:         upstream.ModeOAuth,
			upstreamPath: openai.CodexResponsesPath,
			status:       http.StatusBadGateway,
			duration:     time.Since(start),
			errorDetail:  fmt.Sprintf("translate oauth chat response: %v", handleErr),
			requestBody:  logBody(h.debugDumpHTTP, normalizedBody),
		})
		writeOpenAIError(w, http.StatusBadGateway, "api_error", "Failed to translate upstream response")
		return
	}

	h.logRequest(requestLogFields{
		method:       r.Method,
		path:         r.URL.Path,
		mode:         upstream.ModeOAuth,
		upstreamPath: req.URL.Path,
		status:       http.StatusOK,
		duration:     time.Since(start),
		requestID:    resp.Header.Get("X-Request-ID"),
		requestBody:  logBody(h.debugDumpHTTP, normalizedBody),
	})
}

func (h *OpenAIHandler) doOAuthCodexRequest(r *http.Request, body []byte) (*http.Request, *http.Response, error) {
	if h.credentialsLoader == nil {
		return nil, nil, fmt.Errorf("oauth credentials loader is not configured")
	}

	token, err := h.client.AccessToken(r.Context())
	if err != nil {
		return nil, nil, fmt.Errorf("load oauth access token: %w", err)
	}

	credentials, err := h.credentialsLoader.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load oauth credentials: %w", err)
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.client.CodexResponsesURL(), bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("build oauth upstream request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", firstNonEmpty(r.Header.Get("Accept"), "text/event-stream"))
	req.Header.Set("OpenAI-Beta", openai.CodexOpenAIBeta)
	req.Header.Set("Originator", openai.CodexOriginator)
	req.Header.Set("User-Agent", openai.CodexUserAgent)
	req.Header.Set("Session_ID", firstNonEmpty(sessionIDFromRequest(r.Header), openai.NewSessionID()))
	copyOptionalHeader(req.Header, r.Header, "Accept-Language")
	copyOptionalHeader(req.Header, r.Header, "Conversation_ID")
	copyOptionalHeader(req.Header, r.Header, "X-Codex-Turn-State")
	copyOptionalHeader(req.Header, r.Header, "X-Codex-Turn-Metadata")
	if credentials != nil && strings.TrimSpace(credentials.ChatGPTAccountID) != "" {
		req.Header.Set("ChatGPT-Account-ID", credentials.ChatGPTAccountID)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("oauth upstream request failed: %w", err)
	}
	return req, resp, nil
}

func (h *OpenAIHandler) logRequest(fields requestLogFields) {
	parts := []string{
		"openai-local-gateway",
		fmt.Sprintf("method=%s", fields.method),
		fmt.Sprintf("path=%s", fields.path),
		fmt.Sprintf("mode=%s", firstNonEmpty(fields.mode, "unknown")),
		fmt.Sprintf("status=%d", fields.status),
		fmt.Sprintf("duration_ms=%d", fields.duration.Milliseconds()),
	}
	if fields.upstreamPath != "" {
		parts = append(parts, fmt.Sprintf("upstream=%s", fields.upstreamPath))
	}
	if fields.requestID != "" {
		parts = append(parts, fmt.Sprintf("request_id=%s", fields.requestID))
	}
	if fields.errorDetail != "" {
		parts = append(parts, fmt.Sprintf("error=%q", fields.errorDetail))
	}
	if fields.requestBody != "" {
		parts = append(parts, fmt.Sprintf("request_body=%q", fields.requestBody))
	}
	if fields.responseBody != "" {
		parts = append(parts, fmt.Sprintf("response_body=%q", fields.responseBody))
	}
	if fields.localResponse {
		parts = append(parts, "source=local")
	}
	h.logger.Printf(strings.Join(parts, " "))
}

func (h *OpenAIHandler) mode() string {
	if h.client == nil {
		return "unknown"
	}
	return h.client.Mode()
}

func copyOptionalHeader(dst, src http.Header, key string) {
	if value := strings.TrimSpace(src.Get(key)); value != "" {
		dst.Set(key, value)
	}
}

func sessionIDFromRequest(header http.Header) string {
	for _, key := range []string{"Session_ID", "Session-Id", "session_id"} {
		if value := strings.TrimSpace(header.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func logBody(enabled bool, body []byte) string {
	if !enabled || len(body) == 0 {
		return ""
	}
	if len(body) > 2048 {
		return string(body[:2048]) + "...(truncated)"
	}
	return string(body)
}

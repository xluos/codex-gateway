package upstream

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	ModeAPIKey = "api_key"
	ModeOAuth  = "oauth"
)

type Client struct {
	baseURL     string
	codexBaseURL string
	mode        string
	apiKey      string
	tokenSource interface {
		AccessToken(context.Context) (string, error)
	}
	http *http.Client
}

func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		mode:    ModeAPIKey,
		apiKey:  apiKey,
		http: &http.Client{
			Timeout: timeout,
		},
	}
}

func NewOAuthClient(baseURL string, tokenSource interface {
	AccessToken(context.Context) (string, error)
}, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		codexBaseURL: resolveCodexBaseURL(baseURL),
		mode:         ModeOAuth,
		tokenSource:  tokenSource,
		http: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) NewRequest(ctx context.Context, method, path string, body io.Reader, header http.Header) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}

	copyAllowedRequestHeaders(req.Header, header)
	token, err := c.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req, nil
}

func (c *Client) AccessToken(ctx context.Context) (string, error) {
	if c.tokenSource != nil {
		return c.tokenSource.AccessToken(ctx)
	}
	return c.apiKey, nil
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	return c.http.Do(req)
}

func (c *Client) Mode() string {
	return c.mode
}

func (c *Client) CodexResponsesURL() string {
	return strings.TrimRight(c.codexBaseURL, "/") + "/backend-api/codex/responses"
}

func copyAllowedRequestHeaders(dst, src http.Header) {
	for _, key := range []string{"Content-Type", "Accept", "OpenAI-Beta"} {
		for _, value := range src.Values(key) {
			dst.Add(key, value)
		}
	}
}

func resolveCodexBaseURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "api.openai.com") {
		return "https://chatgpt.com"
	}
	return trimmed
}

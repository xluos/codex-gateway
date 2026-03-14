package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	mathrand "math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

const (
	defaultAuthBaseURL     = "https://auth.openai.com"
	defaultSentinelBaseURL = "https://sentinel.openai.com"
	defaultUserAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"
)

type HTTPPasswordLoginExecutor struct {
	AuthBaseURL     string
	SentinelBaseURL string
	HTTPClient      *http.Client
	StateGenerator  func() string
	Now             func() time.Time
}

type SentinelChallenge struct {
	Token        string               `json:"token"`
	ProofOfWork  SentinelProofOfWork  `json:"proofofwork"`
}

type SentinelProofOfWork struct {
	Required   bool   `json:"required"`
	Seed       string `json:"seed"`
	Difficulty string `json:"difficulty"`
}

type SentinelTokenBuilder struct {
	DeviceID    string
	Now         func() time.Time
	RandomFloat func() float64
	RandomIntn  func(int) int
}

func (e *HTTPPasswordLoginExecutor) Login(ctx context.Context, req PasswordLoginRequest) (*Credentials, error) {
	flow := &CodexPasswordFlow{
		AuthBaseURL:     e.AuthBaseURL,
		SentinelBaseURL: e.SentinelBaseURL,
		HTTPClient:      e.HTTPClient,
		StateGenerator:  e.StateGenerator,
		Now:             e.Now,
	}
	return flow.Login(ctx, req)
}

func (e *HTTPPasswordLoginExecutor) client() (*http.Client, error) {
	if e.HTTPClient != nil {
		if e.HTTPClient.Jar == nil {
			jar, err := cookiejar.New(nil)
			if err != nil {
				return nil, err
			}
			cloned := *e.HTTPClient
			cloned.Jar = jar
			return &cloned, nil
		}
		return e.HTTPClient, nil
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Jar:     jar,
	}, nil
}

func (e *HTTPPasswordLoginExecutor) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func (e *HTTPPasswordLoginExecutor) authorize(ctx context.Context, client *http.Client, authURL string) error {
	noRedirectClient := *client
	noRedirectClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return err
	}
	for k, v := range navigateHeaders() {
		req.Header.Set(k, v)
	}
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		return fmt.Errorf("oauth authorize request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && (resp.StatusCode < 300 || resp.StatusCode >= 400) {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("oauth authorize failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (e *HTTPPasswordLoginExecutor) submitUsername(ctx context.Context, client *http.Client, authBase *url.URL, sentinelBase *url.URL, builder SentinelTokenBuilder, deviceID string, email string) error {
	sentinelToken, err := e.fetchAndBuildSentinelToken(ctx, client, sentinelBase, builder, "authorize_continue")
	if err != nil {
		return err
	}
	body := map[string]any{
		"username": map[string]any{
			"kind":  "email",
			"value": email,
		},
	}
	resp, err := e.doJSON(ctx, client, http.MethodPost, joinURLPath(authBase, "/api/accounts/authorize/continue"), body, buildOpenAIHeaders(joinURLPath(authBase, "/log-in"), deviceID, sentinelToken))
	if err != nil {
		return fmt.Errorf("submit email failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("submit email failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (e *HTTPPasswordLoginExecutor) submitPassword(ctx context.Context, client *http.Client, authBase *url.URL, sentinelBase *url.URL, builder SentinelTokenBuilder, deviceID string, password string) (string, string, error) {
	sentinelToken, err := e.fetchAndBuildSentinelToken(ctx, client, sentinelBase, builder, "password_verify")
	if err != nil {
		return "", "", err
	}
	resp, err := e.doJSON(ctx, client, http.MethodPost, joinURLPath(authBase, "/api/accounts/password/verify"), map[string]any{
		"password": password,
	}, buildOpenAIHeaders(joinURLPath(authBase, "/log-in/password"), deviceID, sentinelToken))
	if err != nil {
		return "", "", fmt.Errorf("submit password failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("submit password failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		ContinueURL string `json:"continue_url"`
		Page        struct {
			Type string `json:"type"`
		} `json:"page"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", "", fmt.Errorf("decode password response: %w", err)
	}
	return payload.ContinueURL, payload.Page.Type, nil
}

func (e *HTTPPasswordLoginExecutor) extractCodeFromContinueURL(ctx context.Context, client *http.Client, authBase *url.URL, deviceID string, continueURL string) (string, error) {
	fullContinueURL := continueURL
	if strings.HasPrefix(continueURL, "/") {
		fullContinueURL = joinURLPath(authBase, continueURL)
	}
	code, err := followAndExtractCode(ctx, client, fullContinueURL)
	if err == nil {
		return code, nil
	}

	sessionData, decodeErr := decodeAuthSessionCookie(client.Jar.Cookies(authBase))
	if decodeErr != nil {
		return "", err
	}
	workspaceID, orgID, projectID := firstWorkspaceAndOrg(sessionData)
	if strings.TrimSpace(workspaceID) == "" {
		return "", err
	}
	code, wsErr := e.selectWorkspaceAndOrganization(ctx, client, authBase, deviceID, fullContinueURL, workspaceID, orgID, projectID)
	if wsErr == nil {
		return code, nil
	}
	return "", err
}

func (e *HTTPPasswordLoginExecutor) selectWorkspaceAndOrganization(ctx context.Context, client *http.Client, authBase *url.URL, deviceID string, referer string, workspaceID string, orgID string, projectID string) (string, error) {
	resp, err := e.doJSONNoRedirect(ctx, client, http.MethodPost, joinURLPath(authBase, "/api/accounts/workspace/select"), map[string]any{
		"workspace_id": workspaceID,
	}, buildOpenAIHeaders(referer, deviceID, ""))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if code, ok := locationCode(resp); ok {
		return code, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("workspace/select failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		ContinueURL string `json:"continue_url"`
		Page        struct {
			Type string `json:"type"`
		} `json:"page"`
		Data struct {
			Orgs []struct {
				ID       string `json:"id"`
				Projects []struct {
					ID string `json:"id"`
				} `json:"projects"`
			} `json:"orgs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode workspace/select response: %w", err)
	}
	if orgID == "" && len(payload.Data.Orgs) > 0 {
		orgID = payload.Data.Orgs[0].ID
		if projectID == "" && len(payload.Data.Orgs[0].Projects) > 0 {
			projectID = payload.Data.Orgs[0].Projects[0].ID
		}
	}
	if strings.Contains(payload.Page.Type, "organization") || strings.Contains(payload.ContinueURL, "organization") || orgID != "" {
		return e.selectOrganization(ctx, client, authBase, deviceID, payload.ContinueURL, orgID, projectID)
	}
	if strings.TrimSpace(payload.ContinueURL) == "" {
		return "", errors.New("workspace/select did not yield continue_url")
	}
	fullNext := payload.ContinueURL
	if strings.HasPrefix(fullNext, "/") {
		fullNext = joinURLPath(authBase, fullNext)
	}
	return followAndExtractCode(ctx, client, fullNext)
}

func (e *HTTPPasswordLoginExecutor) selectOrganization(ctx context.Context, client *http.Client, authBase *url.URL, deviceID string, referer string, orgID string, projectID string) (string, error) {
	if strings.TrimSpace(orgID) == "" {
		return "", errors.New("organization/select requires org_id")
	}
	body := map[string]any{"org_id": orgID}
	if strings.TrimSpace(projectID) != "" {
		body["project_id"] = projectID
	}
	resp, err := e.doJSONNoRedirect(ctx, client, http.MethodPost, joinURLPath(authBase, "/api/accounts/organization/select"), body, buildOpenAIHeaders(firstNonEmptyString(referer, joinURLPath(authBase, "/sign-in-with-chatgpt/codex/consent")), deviceID, ""))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if code, ok := locationCode(resp); ok {
		return code, nil
	}
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 131072))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("organization/select failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	var payload struct {
		ContinueURL string `json:"continue_url"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return "", fmt.Errorf("decode organization/select response: %w", err)
	}
	if strings.TrimSpace(payload.ContinueURL) == "" {
		return "", errors.New("organization/select did not yield continue_url")
	}
	fullNext := payload.ContinueURL
	if strings.HasPrefix(fullNext, "/") {
		fullNext = joinURLPath(authBase, fullNext)
	}
	return followAndExtractCode(ctx, client, fullNext)
}

func (e *HTTPPasswordLoginExecutor) exchangeCode(ctx context.Context, client *http.Client, authBase *url.URL, req PasswordLoginRequest, code string, codeVerifier string) (*Credentials, error) {
	tokenURL := joinURLPath(authBase, "/oauth/token")
	flow := NewFlow(Config{
		AuthorizeURL: joinURLPath(authBase, "/oauth/authorize"),
		TokenURL:     tokenURL,
		RedirectURI:  req.RedirectURI,
		ClientID:     firstNonEmptyString(req.ClientID, DefaultClientID),
		Scopes:       DefaultScopes,
	}, client)
	session := &AuthURLResult{
		State:        "password-oauth",
		CodeVerifier: codeVerifier,
		RedirectURI:  req.RedirectURI,
		ClientID:     firstNonEmptyString(req.ClientID, DefaultClientID),
	}
	cred, err := flow.exchange(ctx, url.Values{
		"grant_type":    []string{"authorization_code"},
		"client_id":     []string{session.ClientID},
		"code":          []string{code},
		"redirect_uri":  []string{session.RedirectURI},
		"code_verifier": []string{session.CodeVerifier},
	}, session.ClientID)
	if err != nil {
		return nil, err
	}
	if cred.Email == "" {
		cred.Email = req.Email
	}
	return cred, nil
}

func (e *HTTPPasswordLoginExecutor) doJSON(ctx context.Context, client *http.Client, method string, targetURL string, body any, headers map[string]string) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return client.Do(req)
}

func (e *HTTPPasswordLoginExecutor) doJSONNoRedirect(ctx context.Context, client *http.Client, method string, targetURL string, body any, headers map[string]string) (*http.Response, error) {
	noRedirectClient := *client
	noRedirectClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return e.doJSON(ctx, &noRedirectClient, method, targetURL, body, headers)
}

func (e *HTTPPasswordLoginExecutor) fetchAndBuildSentinelToken(ctx context.Context, client *http.Client, sentinelBase *url.URL, builder SentinelTokenBuilder, flow string) (string, error) {
	requirementsToken, err := builder.BuildRequirementsToken()
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"p":    requirementsToken,
		"id":   builder.DeviceID,
		"flow": flow,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURLPath(sentinelBase, "/backend-api/sentinel/req"), strings.NewReader(mustJSON(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req.Header.Set("Referer", joinURLPath(sentinelBase, "/backend-api/sentinel/frame.html"))
	req.Header.Set("Origin", strings.TrimRight(sentinelBase.String(), "/"))
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("sec-ch-ua", `"Not:A-Brand";v="99", "Google Chrome";v="145", "Chromium";v="145"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch sentinel challenge: %w", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 131072))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch sentinel challenge failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	var challenge SentinelChallenge
	if err := json.Unmarshal(bodyBytes, &challenge); err != nil {
		return "", fmt.Errorf("decode sentinel challenge: %w", err)
	}
	return builder.BuildToken(challenge, flow)
}

func (b SentinelTokenBuilder) BuildToken(challenge SentinelChallenge, flow string) (string, error) {
	p := ""
	var err error
	if challenge.ProofOfWork.Required && strings.TrimSpace(challenge.ProofOfWork.Seed) != "" {
		p, err = b.generateToken(challenge.ProofOfWork.Seed, firstNonEmptyString(challenge.ProofOfWork.Difficulty, "0"))
		if err != nil {
			return "", err
		}
	} else {
		p, err = b.BuildRequirementsToken()
		if err != nil {
			return "", err
		}
	}
	payload, err := json.Marshal(map[string]string{
		"p":    p,
		"t":    "",
		"c":    challenge.Token,
		"id":   b.deviceID(),
		"flow": flow,
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func (b SentinelTokenBuilder) BuildRequirementsToken() (string, error) {
	config, err := b.config()
	if err != nil {
		return "", err
	}
	config[3] = float64(1)
	config[9] = float64(5 + b.randomFloat()*45)
	data, err := base64EncodeJSON(config)
	if err != nil {
		return "", err
	}
	return "gAAAAAC" + data, nil
}

func (b SentinelTokenBuilder) generateToken(seed string, difficulty string) (string, error) {
	config, err := b.config()
	if err != nil {
		return "", err
	}
	start := b.now()
	for nonce := 0; nonce < 500000; nonce++ {
		config[3] = nonce
		config[9] = int(math.Round(float64(b.now().Sub(start).Milliseconds())))
		data, err := base64EncodeJSON(config)
		if err != nil {
			return "", err
		}
		hash := fnv1a32(seed + data)
		if hashPrefixLE(hash, difficulty) {
			return "gAAAAAB" + data + "~S", nil
		}
	}
	return "", errors.New("sentinel proof-of-work exceeded max attempts")
}

func (b SentinelTokenBuilder) config() ([]any, error) {
	sessionID, err := generateUUID()
	if err != nil {
		return nil, err
	}
	now := b.now()
	perfNow := 1000 + b.randomFloat()*49000
	timeOrigin := float64(now.UnixMilli()) - perfNow
	navProps := []string{"vendorSub", "productSub", "vendor", "maxTouchPoints", "scheduling", "userActivation", "doNotTrack", "geolocation", "connection", "plugins", "mimeTypes", "pdfViewerEnabled", "webkitTemporaryStorage", "webkitPersistentStorage", "hardwareConcurrency", "cookieEnabled", "credentials", "mediaDevices", "permissions", "locks", "ink"}
	docKeys := []string{"location", "implementation", "URL", "documentURI", "compatMode"}
	winKeys := []string{"Object", "Function", "Array", "Number", "parseFloat", "undefined"}
	cores := []int{4, 8, 12, 16}
	config := []any{
		"1920x1080",
		now.Format("Mon Jan 02 2006 15:04:05 GMT+0000 (Coordinated Universal Time)"),
		4294705152,
		b.randomFloat(),
		defaultUserAgent,
		"https://sentinel.openai.com/sentinel/20260124ceb8/sdk.js",
		nil,
		nil,
		"en-US",
		"en-US,en",
		b.randomFloat(),
		navProps[b.randomIntn(len(navProps))] + "−undefined",
		docKeys[b.randomIntn(len(docKeys))],
		winKeys[b.randomIntn(len(winKeys))],
		perfNow,
		sessionID,
		"",
		cores[b.randomIntn(len(cores))],
		timeOrigin,
	}
	return config, nil
}

func (b SentinelTokenBuilder) now() time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

func (b SentinelTokenBuilder) randomFloat() float64 {
	if b.RandomFloat != nil {
		return b.RandomFloat()
	}
	return mathrand.New(mathrand.NewSource(time.Now().UnixNano())).Float64()
}

func (b SentinelTokenBuilder) randomIntn(max int) int {
	if max <= 1 {
		return 0
	}
	if b.RandomIntn != nil {
		return b.RandomIntn(max)
	}
	return mathrand.New(mathrand.NewSource(time.Now().UnixNano())).Intn(max)
}

func (b SentinelTokenBuilder) deviceID() string {
	if strings.TrimSpace(b.DeviceID) != "" {
		return b.DeviceID
	}
	id, err := generateUUID()
	if err != nil {
		return "device"
	}
	return id
}

func buildAuthorizeURL(authBase *url.URL, clientID string, redirectURI string, state string, codeChallenge string) string {
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", firstNonEmptyString(clientID, DefaultClientID))
	params.Set("redirect_uri", firstNonEmptyString(redirectURI, DefaultRedirect))
	params.Set("scope", DefaultScopes)
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("id_token_add_organizations", "true")
	params.Set("codex_cli_simplified_flow", "true")
	return joinURLPath(authBase, "/oauth/authorize") + "?" + params.Encode()
}

func joinURLPath(base *url.URL, path string) string {
	if base == nil {
		return path
	}
	clone := *base
	clone.Path = strings.TrimRight(clone.Path, "/") + path
	clone.RawQuery = ""
	clone.Fragment = ""
	return clone.String()
}

func buildOpenAIHeaders(referer string, deviceID string, sentinelToken string) map[string]string {
	headers := map[string]string{
		"accept":             "application/json",
		"accept-language":    "en-US,en;q=0.9",
		"content-type":       "application/json",
		"origin":             defaultAuthBaseURL,
		"user-agent":         defaultUserAgent,
		"sec-ch-ua":          `"Google Chrome";v="145", "Not?A_Brand";v="8", "Chromium";v="145"`,
		"sec-ch-ua-mobile":   "?0",
		"sec-ch-ua-platform": `"Windows"`,
		"sec-fetch-dest":     "empty",
		"sec-fetch-mode":     "cors",
		"sec-fetch-site":     "same-origin",
		"referer":            referer,
		"oai-device-id":      deviceID,
	}
	for k, v := range generateDatadogTrace() {
		headers[k] = v
	}
	if strings.TrimSpace(sentinelToken) != "" {
		headers["openai-sentinel-token"] = sentinelToken
	}
	return headers
}

func navigateHeaders() map[string]string {
	return map[string]string{
		"accept":                     "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		"accept-language":            "en-US,en;q=0.9",
		"user-agent":                 defaultUserAgent,
		"sec-ch-ua":                  `"Google Chrome";v="145", "Not?A_Brand";v="8", "Chromium";v="145"`,
		"sec-ch-ua-mobile":           "?0",
		"sec-ch-ua-platform":         `"Windows"`,
		"sec-fetch-dest":             "document",
		"sec-fetch-mode":             "navigate",
		"sec-fetch-site":             "same-origin",
		"sec-fetch-user":             "?1",
		"upgrade-insecure-requests":  "1",
	}
}

func generateDatadogTrace() map[string]string {
	traceID := pseudoRandomUint64()
	parentID := pseudoRandomUint64()
	return map[string]string{
		"traceparent":                fmt.Sprintf("00-0000000000000000%016x-%016x-01", traceID, parentID),
		"tracestate":                 "dd=s:1;o:rum",
		"x-datadog-origin":           "rum",
		"x-datadog-parent-id":        fmt.Sprintf("%d", parentID),
		"x-datadog-sampling-priority":"1",
		"x-datadog-trace-id":         fmt.Sprintf("%d", traceID),
	}
}

func pseudoRandomUint64() uint64 {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err == nil {
		var out uint64
		for _, b := range buf {
			out = (out << 8) | uint64(b)
		}
		return out
	}
	return uint64(time.Now().UnixNano())
}

func setDeviceCookie(jar http.CookieJar, authBase *url.URL, deviceID string) {
	if jar == nil || authBase == nil {
		return
	}
	jar.SetCookies(authBase, []*http.Cookie{{Name: "oai-did", Value: deviceID, Path: "/"}})
}

func followAndExtractCode(ctx context.Context, client *http.Client, targetURL string) (string, error) {
	noRedirectClient := *client
	noRedirectClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	currentURL := targetURL
	for depth := 0; depth < 10; depth++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentURL, nil)
		if err != nil {
			return "", err
		}
		for k, v := range navigateHeaders() {
			req.Header.Set(k, v)
		}
		resp, err := noRedirectClient.Do(req)
		if err != nil {
			return "", err
		}
		resp.Body.Close()
		if code, ok := locationCode(resp); ok {
			return code, nil
		}
		if resp.StatusCode < 300 || resp.StatusCode >= 400 {
			if code, err := extractAuthorizationCode(resp.Request.URL.String()); err == nil {
				return code, nil
			}
			return "", errors.New("authorization code not found in continue flow")
		}
		loc := resp.Header.Get("Location")
		if strings.TrimSpace(loc) == "" {
			break
		}
		nextURL, err := resp.Request.URL.Parse(loc)
		if err != nil {
			return "", err
		}
		currentURL = nextURL.String()
	}
	return "", errors.New("authorization code not found in continue flow")
}

func locationCode(resp *http.Response) (string, bool) {
	if resp == nil {
		return "", false
	}
	loc := resp.Header.Get("Location")
	if strings.TrimSpace(loc) == "" {
		return "", false
	}
	target, err := resp.Request.URL.Parse(loc)
	if err != nil {
		return "", false
	}
	code, err := extractAuthorizationCode(target.String())
	return code, err == nil
}

func extractAuthorizationCode(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	code := strings.TrimSpace(parsed.Query().Get("code"))
	if code == "" {
		return "", errors.New("authorization code not found")
	}
	return code, nil
}

func decodeAuthSessionCookie(cookies []*http.Cookie) (map[string]any, error) {
	for _, cookie := range cookies {
		if cookie.Name != "oai-client-auth-session" {
			continue
		}
		raw := cookie.Value
		if idx := strings.Index(raw, "."); idx >= 0 {
			raw = raw[:idx]
		}
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			return nil, err
		}
		var payload map[string]any
		if err := json.Unmarshal(decoded, &payload); err != nil {
			return nil, err
		}
		return payload, nil
	}
	return nil, errors.New("oai-client-auth-session cookie not found")
}

func firstWorkspaceAndOrg(sessionData map[string]any) (string, string, string) {
	workspaces, _ := sessionData["workspaces"].([]any)
	if len(workspaces) == 0 {
		return "", "", ""
	}
	workspace, _ := workspaces[0].(map[string]any)
	workspaceID, _ := workspace["id"].(string)
	orgs, _ := sessionData["orgs"].([]any)
	if len(orgs) == 0 {
		return workspaceID, "", ""
	}
	org, _ := orgs[0].(map[string]any)
	orgID, _ := org["id"].(string)
	projects, _ := org["projects"].([]any)
	if len(projects) == 0 {
		return workspaceID, orgID, ""
	}
	project, _ := projects[0].(map[string]any)
	projectID, _ := project["id"].(string)
	return workspaceID, orgID, projectID
}

func generateUUID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4],
		buf[4:6],
		buf[6:8],
		buf[8:10],
		buf[10:16],
	), nil
}

func mustJSON(v any) string {
	payload, _ := json.Marshal(v)
	return string(payload)
}

func base64EncodeJSON(v any) (string, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(payload), nil
}

func fnv1a32(text string) string {
	var h uint32 = 2166136261
	for _, ch := range text {
		h ^= uint32(ch)
		h *= 16777619
	}
	h ^= h >> 16
	h *= 2246822507
	h ^= h >> 13
	h *= 3266489909
	h ^= h >> 16
	return fmt.Sprintf("%08x", h)
}

func hashPrefixLE(hash string, difficulty string) bool {
	if difficulty == "" {
		difficulty = "0"
	}
	prefixLen := len(difficulty)
	if len(hash) < prefixLen {
		return false
	}
	return hash[:prefixLen] <= difficulty
}

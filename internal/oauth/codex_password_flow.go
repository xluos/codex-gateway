package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type OTPProvider interface {
	Codes(ctx context.Context, email string) ([]string, error)
}

type CodexPasswordFlow struct {
	AuthBaseURL     string
	SentinelBaseURL string
	HTTPClient      *http.Client
	StateGenerator  func() string
	Now             func() time.Time
	OTPProvider     OTPProvider
}

func (f *CodexPasswordFlow) Login(ctx context.Context, req PasswordLoginRequest) (*Credentials, error) {
	exec := &HTTPPasswordLoginExecutor{
		AuthBaseURL:     f.AuthBaseURL,
		SentinelBaseURL: f.SentinelBaseURL,
		HTTPClient:      f.HTTPClient,
		StateGenerator:  f.StateGenerator,
		Now:             f.Now,
	}
	client, err := exec.client()
	if err != nil {
		return nil, err
	}
	authBase, err := url.Parse(firstNonEmptyString(f.AuthBaseURL, defaultAuthBaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse auth base url: %w", err)
	}
	sentinelBase, err := url.Parse(firstNonEmptyString(f.SentinelBaseURL, defaultSentinelBaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse sentinel base url: %w", err)
	}

	deviceID, err := generateUUID()
	if err != nil {
		return nil, fmt.Errorf("generate device id: %w", err)
	}
	setDeviceCookie(client.Jar, authBase, deviceID)

	state := ""
	if f.StateGenerator != nil {
		state = f.StateGenerator()
	}
	if strings.TrimSpace(state) == "" {
		state, err = generateHex(32)
		if err != nil {
			return nil, err
		}
	}
	codeVerifier, err := generateHex(64)
	if err != nil {
		return nil, err
	}
	codeChallenge := generateCodeChallenge(codeVerifier)
	authURL := buildAuthorizeURL(authBase, req.ClientID, req.RedirectURI, state, codeChallenge)
	if err := exec.authorize(ctx, client, authURL); err != nil {
		return nil, err
	}

	tokenBuilder := SentinelTokenBuilder{DeviceID: deviceID, Now: exec.now}
	if err := exec.submitUsername(ctx, client, authBase, sentinelBase, tokenBuilder, deviceID, req.Email); err != nil {
		return nil, err
	}
	continueURL, pageType, err := exec.submitPassword(ctx, client, authBase, sentinelBase, tokenBuilder, deviceID, req.Password)
	if err != nil {
		return nil, err
	}
	continueURL, pageType, err = f.handleEmailOTP(ctx, client, authBase, deviceID, req.Email, continueURL, pageType)
	if err != nil {
		return nil, err
	}
	continueURL, err = f.handleAboutYou(ctx, client, authBase, deviceID, continueURL)
	if err != nil {
		return nil, err
	}
	if strings.Contains(pageType, "consent") && strings.TrimSpace(continueURL) == "" {
		continueURL = joinURLPath(authBase, "/sign-in-with-chatgpt/codex/consent")
	}
	if strings.TrimSpace(continueURL) == "" {
		return nil, errors.New("missing continue_url from password flow")
	}

	code, err := exec.extractCodeFromContinueURL(ctx, client, authBase, deviceID, continueURL)
	if err != nil {
		return nil, err
	}
	return exec.exchangeCode(ctx, client, authBase, req, code, codeVerifier)
}

func (f *CodexPasswordFlow) handleEmailOTP(ctx context.Context, client *http.Client, authBase *url.URL, deviceID string, email string, continueURL string, pageType string) (string, string, error) {
	if pageType != "email_otp_verification" && !strings.Contains(continueURL, "email-verification") {
		return continueURL, pageType, nil
	}
	if f.OTPProvider == nil {
		return "", "", errors.New("email otp verification requires an OTP provider")
	}
	codes, err := f.OTPProvider.Codes(ctx, email)
	if err != nil {
		return "", "", fmt.Errorf("load email otp codes: %w", err)
	}
	headers := buildOpenAIHeaders(joinURLPath(authBase, "/email-verification"), deviceID, "")
	for _, code := range codes {
		resp, err := doJSONRequest(ctx, client, http.MethodPost, joinURLPath(authBase, "/api/accounts/email-otp/validate"), map[string]any{"code": code}, headers)
		if err != nil {
			return "", "", err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		var payload struct {
			ContinueURL string `json:"continue_url"`
			Page        struct {
				Type string `json:"type"`
			} `json:"page"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return "", "", fmt.Errorf("decode email otp response: %w", err)
		}
		return payload.ContinueURL, payload.Page.Type, nil
	}
	return "", "", errors.New("email otp verification failed")
}

func (f *CodexPasswordFlow) handleAboutYou(ctx context.Context, client *http.Client, authBase *url.URL, deviceID string, continueURL string) (string, error) {
	if !strings.Contains(continueURL, "about-you") {
		return continueURL, nil
	}
	aboutURL := joinURLPath(authBase, "/about-you")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, aboutURL, nil)
	if err != nil {
		return "", err
	}
	for k, v := range navigateHeaders() {
		req.Header.Set(k, v)
	}
	req.Header.Set("referer", joinURLPath(authBase, "/email-verification"))
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if strings.Contains(resp.Request.URL.String(), "consent") || strings.Contains(resp.Request.URL.String(), "organization") {
		return resp.Request.URL.String(), nil
	}
	name, birthdate := randomProfile()
	createHeaders := buildOpenAIHeaders(aboutURL, deviceID, "")
	createResp, err := doJSONRequest(ctx, client, http.MethodPost, joinURLPath(authBase, "/api/accounts/create_account"), map[string]any{
		"name":      name,
		"birthdate": birthdate,
	}, createHeaders)
	if err != nil {
		return "", err
	}
	body, _ := io.ReadAll(io.LimitReader(createResp.Body, 4096))
	createResp.Body.Close()
	if createResp.StatusCode == http.StatusBadRequest && strings.Contains(string(body), "already_exists") {
		return joinURLPath(authBase, "/sign-in-with-chatgpt/codex/consent"), nil
	}
	if createResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("create_account failed with status %d: %s", createResp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		ContinueURL string `json:"continue_url"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode create_account response: %w", err)
	}
	return payload.ContinueURL, nil
}

func doJSONRequest(ctx context.Context, client *http.Client, method string, targetURL string, body any, headers map[string]string) (*http.Response, error) {
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

func randomProfile() (string, string) {
	firstNames := []string{"James", "Mary", "John", "Linda", "Robert", "Sarah"}
	lastNames := []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Wilson"}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	name := firstNames[r.Intn(len(firstNames))] + " " + lastNames[r.Intn(len(lastNames))]
	year := 1995 + r.Intn(8)
	month := 1 + r.Intn(12)
	day := 1 + r.Intn(28)
	return name, fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

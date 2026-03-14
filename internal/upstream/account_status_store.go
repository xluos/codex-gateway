package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codex-gateway/internal/openai"
	"codex-gateway/internal/oauth"
)

const accountStatusFileName = "accounts-status.json"

type accountStatusFile struct {
	UpdatedAt time.Time       `json:"updated_at"`
	Accounts  []AccountStatus `json:"accounts"`
}

func AccountStatusPath(runtimeDir string) string {
	return filepath.Join(strings.TrimSpace(runtimeDir), accountStatusFileName)
}

func SaveAccountStatuses(path string, statuses []AccountStatus) error {
	payload := accountStatusFile{
		UpdatedAt: time.Now().UTC(),
		Accounts:  statuses,
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func LoadAccountStatuses(path string) ([]AccountStatus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload accountStatusFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload.Accounts, nil
}

func (p *OpenAIAccountPool) ApplyPersistedStatuses(statuses []AccountStatus) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, status := range statuses {
		state := p.state[status.Name]
		if state == nil {
			continue
		}
		state.cooldownUntil = cloneTimePtr(status.CooldownUntil)
		state.lastError = status.LastError
		state.snapshotUpdatedAt = cloneTimePtr(status.SnapshotUpdatedAt)
		state.codex5hUsed = cloneFloat64Ptr(status.Codex5hUsedPercent)
		state.codex5hResetAt = cloneTimePtr(status.Codex5hResetAt)
		state.codex7dUsed = cloneFloat64Ptr(status.Codex7dUsedPercent)
		state.codex7dResetAt = cloneTimePtr(status.Codex7dResetAt)
	}
}

func (p *OpenAIAccountPool) ProbeAccounts(ctx context.Context, names []string) error {
	var failures []string
	for _, name := range names {
		if err := p.probeAccount(ctx, name); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return errors.New(strings.Join(failures, "; "))
}

func (p *OpenAIAccountPool) ProbeAccount(ctx context.Context, name string) error {
	return p.probeAccount(ctx, name)
}

func (p *OpenAIAccountPool) probeAccount(ctx context.Context, name string) error {
	account := p.accountByName(name)
	if account == nil {
		return fmt.Errorf("%s: account not found", name)
	}
	req, err := buildProbeRequest(ctx, account)
	if err != nil {
		p.setLastError(name, err.Error())
		return fmt.Errorf("%s: %w", name, err)
	}
	resp, err := account.Client.Do(req)
	if err != nil {
		p.setLastError(name, err.Error())
		return fmt.Errorf("%s: %w", name, err)
	}
	defer resp.Body.Close()

	p.UpdateSnapshot(name, resp.Header)
	if hasUsableSnapshotHeaders(resp.Header) {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	p.setLastError(name, message)
	return fmt.Errorf("%s: probe failed with %s", name, resp.Status)
}

func buildProbeRequest(ctx context.Context, account *PoolAccount) (*http.Request, error) {
	body := []byte(fmt.Sprintf(`{"model":%q,"input":"ping","max_output_tokens":1}`, probeModel(account)))
	if IsOAuthPoolMode(account.Mode) {
		normalizedBody, err := openai.NormalizeCodexResponsesRequest(body)
		if err != nil {
			return nil, err
		}
		token, err := account.Client.AccessToken(ctx)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, account.Client.CodexResponsesURL(), strings.NewReader(string(normalizedBody)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("OpenAI-Beta", openai.CodexOpenAIBeta)
		req.Header.Set("Originator", openai.CodexOriginator)
		req.Header.Set("Version", "0.104.0")
		req.Header.Set("User-Agent", openai.CodexUserAgent)
		req.Header.Set("Session_ID", openai.NewSessionID())
		if strings.TrimSpace(account.OAuthCredentialsFile) != "" {
			if cred, err := oauth.NewStore(account.OAuthCredentialsFile).Load(); err == nil && strings.TrimSpace(cred.ChatGPTAccountID) != "" {
				req.Header.Set("ChatGPT-Account-ID", cred.ChatGPTAccountID)
			}
		}
		return req, nil
	}
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	return account.Client.NewRequest(ctx, http.MethodPost, "/v1/responses", strings.NewReader(string(body)), header)
}

func probeModel(account *PoolAccount) string {
	if account == nil {
		return "gpt-4.1-mini"
	}
	if model := strings.TrimSpace(account.DefaultModel); model != "" {
		return model
	}
	for _, model := range account.ModelMapping {
		if strings.TrimSpace(model) != "" {
			return strings.TrimSpace(model)
		}
	}
	if IsOAuthPoolMode(account.Mode) {
		return "gpt-5.1-codex-mini"
	}
	return "gpt-4.1-mini"
}

func hasUsableSnapshotHeaders(headers http.Header) bool {
	return ParseCodexSnapshot(headers, time.Now().UTC()) != nil
}

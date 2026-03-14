package upstream

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"codex-gateway/internal/config"
)

type AccessTokenSource interface {
	AccessToken(context.Context) (string, error)
}

type PoolAccount struct {
	Name                 string
	Mode                 string
	BaseURL              string
	APIKey               string
	OAuthCredentialsFile string
	Priority             int
	DefaultModel         string
	ModelMapping         map[string]string
	Cooldown             time.Duration
	Client               *Client
}

type AccountStatus struct {
	Name               string     `json:"name"`
	Mode               string     `json:"mode"`
	Priority           int        `json:"priority"`
	DefaultModel       string     `json:"default_model"`
	Status             string     `json:"status"`
	CooldownUntil      *time.Time `json:"cooldown_until"`
	LastError          string     `json:"last_error"`
	SnapshotUpdatedAt  *time.Time `json:"snapshot_updated_at"`
	Codex5hUsedPercent *float64   `json:"codex_5h_used_percent"`
	Codex5hResetAt     *time.Time `json:"codex_5h_reset_at"`
	Codex7dUsedPercent *float64   `json:"codex_7d_used_percent"`
	Codex7dResetAt     *time.Time `json:"codex_7d_reset_at"`
}

type accountRuntimeState struct {
	cooldownUntil     *time.Time
	lastError         string
	snapshotUpdatedAt *time.Time
	codex5hUsed       *float64
	codex5hResetAt    *time.Time
	codex7dUsed       *float64
	codex7dResetAt    *time.Time
}

type OpenAIAccountPool struct {
	mu       sync.Mutex
	accounts []PoolAccount
	rr       map[int]int
	state    map[string]*accountRuntimeState
	timeout  time.Duration
}

func NewOpenAIAccountPool(cfgs []config.NamedUpstreamConfig, tokenSources map[string]AccessTokenSource, timeout time.Duration) *OpenAIAccountPool {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	pool := &OpenAIAccountPool{
		rr:      make(map[int]int),
		state:   make(map[string]*accountRuntimeState),
		timeout: timeout,
	}
	for _, cfg := range cfgs {
		account := PoolAccount{
			Name:                 cfg.Name,
			Mode:                 cfg.Mode,
			BaseURL:              cfg.BaseURL,
			APIKey:               cfg.APIKey,
			OAuthCredentialsFile: cfg.OAuth.CredentialsFile,
			Priority:             cfg.Priority,
			DefaultModel:         cfg.DefaultModel,
			ModelMapping:         copyModelMapping(cfg.ModelMapping),
			Cooldown:             time.Duration(cfg.CooldownSeconds) * time.Second,
		}
		switch cfg.Mode {
		case ModeOAuth, "password_oauth":
			account.Client = NewOAuthClient(cfg.BaseURL, tokenSources[cfg.Name], timeout)
		default:
			account.Client = NewClient(cfg.BaseURL, cfg.APIKey, timeout)
		}
		pool.accounts = append(pool.accounts, account)
		pool.state[account.Name] = &accountRuntimeState{}
	}
	sort.SliceStable(pool.accounts, func(i, j int) bool {
		if pool.accounts[i].Priority == pool.accounts[j].Priority {
			return pool.accounts[i].Name < pool.accounts[j].Name
		}
		return pool.accounts[i].Priority < pool.accounts[j].Priority
	})
	return pool
}

func (p *OpenAIAccountPool) Select(requestedModel string, excluded map[string]struct{}) (*PoolAccount, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	grouped := make(map[int][]int)
	var priorities []int
	for i := range p.accounts {
		account := &p.accounts[i]
		if excluded != nil {
			if _, ok := excluded[account.Name]; ok {
				continue
			}
		}
		state := p.state[account.Name]
		if state != nil && state.cooldownUntil != nil && now.Before(*state.cooldownUntil) {
			continue
		}
		if !supportsModel(account, requestedModel) {
			continue
		}
		grouped[account.Priority] = append(grouped[account.Priority], i)
	}
	for priority := range grouped {
		priorities = append(priorities, priority)
	}
	sort.Ints(priorities)
	for _, priority := range priorities {
		indices := grouped[priority]
		if len(indices) == 0 {
			continue
		}
		start := p.rr[priority] % len(indices)
		for offset := 0; offset < len(indices); offset++ {
			idx := indices[(start+offset)%len(indices)]
			account := &p.accounts[idx]
			resolved := resolveModel(account, requestedModel)
			if strings.TrimSpace(resolved) == "" {
				continue
			}
			p.rr[priority] = (start + offset + 1) % len(indices)
			return account, resolved, nil
		}
	}
	return nil, "", errors.New("no available upstream accounts")
}

func (p *OpenAIAccountPool) MarkCooldown(name string, until time.Time, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.state[name]
	if state == nil {
		return
	}
	u := until.UTC()
	state.cooldownUntil = &u
	state.lastError = reason
}

func (p *OpenAIAccountPool) UpdateSnapshot(name string, headers http.Header) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.state[name]
	if state == nil {
		return
	}
	now := time.Now().UTC()
	state.snapshotUpdatedAt = &now
	state.lastError = ""
	if snapshot := ParseCodexSnapshot(headers, now); snapshot != nil {
		state.codex5hUsed = cloneFloat64Ptr(snapshot.Codex5hUsedPercent)
		state.codex5hResetAt = cloneTimePtr(snapshot.Codex5hResetAt)
		state.codex7dUsed = cloneFloat64Ptr(snapshot.Codex7dUsedPercent)
		state.codex7dResetAt = cloneTimePtr(snapshot.Codex7dResetAt)
	}
}

func (p *OpenAIAccountPool) AccountsStatus() []AccountStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	statuses := make([]AccountStatus, 0, len(p.accounts))
	for _, account := range p.accounts {
		state := p.state[account.Name]
		status := "available"
		if state == nil {
			status = "unknown"
		} else if state.cooldownUntil != nil && now.Before(*state.cooldownUntil) {
			status = "cooldown"
		} else if state.codex5hResetAt != nil && now.Before(*state.codex5hResetAt) {
			status = "rate_limited"
		} else if state.codex7dResetAt != nil && now.Before(*state.codex7dResetAt) {
			status = "rate_limited"
		}
		statuses = append(statuses, AccountStatus{
			Name:               account.Name,
			Mode:               account.Mode,
			Priority:           account.Priority,
			DefaultModel:       account.DefaultModel,
			Status:             status,
			CooldownUntil:      cloneTimePtr(state.cooldownUntil),
			LastError:          state.lastError,
			SnapshotUpdatedAt:  cloneTimePtr(state.snapshotUpdatedAt),
			Codex5hUsedPercent: cloneFloat64Ptr(state.codex5hUsed),
			Codex5hResetAt:     cloneTimePtr(state.codex5hResetAt),
			Codex7dUsedPercent: cloneFloat64Ptr(state.codex7dUsed),
			Codex7dResetAt:     cloneTimePtr(state.codex7dResetAt),
		})
	}
	return statuses
}

func (p *OpenAIAccountPool) accountByName(name string) *PoolAccount {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.accounts {
		if p.accounts[i].Name == name {
			return &p.accounts[i]
		}
	}
	return nil
}

func (p *OpenAIAccountPool) setLastError(name, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if state := p.state[name]; state != nil {
		state.lastError = strings.TrimSpace(message)
	}
}

func IsOAuthPoolMode(mode string) bool {
	return mode == ModeOAuth || mode == "password_oauth"
}

func copyModelMapping(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func supportsModel(account *PoolAccount, requestedModel string) bool {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return strings.TrimSpace(account.DefaultModel) != ""
	}
	if len(account.ModelMapping) == 0 {
		return true
	}
	_, ok := account.ModelMapping[requestedModel]
	return ok
}

func resolveModel(account *PoolAccount, requestedModel string) string {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return strings.TrimSpace(account.DefaultModel)
	}
	if mapped, ok := account.ModelMapping[requestedModel]; ok && strings.TrimSpace(mapped) != "" {
		return mapped
	}
	return requestedModel
}

func parseFloatHeader(headers http.Header, key string) (float64, bool) {
	value := strings.TrimSpace(headers.Get(key))
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func parseSecondsReset(headers http.Header, key string, base time.Time) (time.Time, bool) {
	value := strings.TrimSpace(headers.Get(key))
	if value == "" {
		return time.Time{}, false
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return time.Time{}, false
	}
	reset := base.Add(time.Duration(seconds) * time.Second).UTC()
	return reset, true
}

func cloneTimePtr(src *time.Time) *time.Time {
	if src == nil {
		return nil
	}
	dup := src.UTC()
	return &dup
}

func cloneFloat64Ptr(src *float64) *float64 {
	if src == nil {
		return nil
	}
	dup := *src
	return &dup
}

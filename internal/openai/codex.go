package openai

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const (
	CodexResponsesPath = "/backend-api/codex/responses"
	CodexOriginator    = "codex_cli_rs"
	CodexUserAgent     = "codex_cli_rs/0.104.0"
	CodexOpenAIBeta    = "responses=experimental"

	defaultInstructions = "You are a helpful coding assistant."
)

type Model struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created"`
	OwnedBy     string `json:"owned_by"`
	Type        string `json:"type,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

var DefaultModels = []Model{
	{ID: "gpt-5.4", Object: "model", Created: 1738368000, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.4"},
	{ID: "gpt-5.3-codex", Object: "model", Created: 1735689600, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.3 Codex"},
	{ID: "gpt-5.3-codex-spark", Object: "model", Created: 1735689600, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.3 Codex Spark"},
	{ID: "gpt-5.2", Object: "model", Created: 1733875200, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.2"},
	{ID: "gpt-5.2-codex", Object: "model", Created: 1733011200, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.2 Codex"},
	{ID: "gpt-5.1-codex-max", Object: "model", Created: 1730419200, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.1 Codex Max"},
	{ID: "gpt-5.1-codex", Object: "model", Created: 1730419200, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.1 Codex"},
	{ID: "gpt-5.1", Object: "model", Created: 1731456000, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.1"},
	{ID: "gpt-5.1-codex-mini", Object: "model", Created: 1730419200, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.1 Codex Mini"},
	{ID: "gpt-5", Object: "model", Created: 1722988800, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5"},
}

func DefaultModelsResponse() map[string]any {
	return map[string]any{
		"object": "list",
		"data":   DefaultModels,
	}
}

func NormalizeCodexResponsesRequest(body []byte) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}

	if model, ok := req["model"].(string); ok {
		req["model"] = normalizeCodexModel(model)
	}
	if _, ok := req["instructions"]; !ok {
		req["instructions"] = defaultInstructions
	} else if instructions, ok := req["instructions"].(string); ok && strings.TrimSpace(instructions) == "" {
		req["instructions"] = defaultInstructions
	}

	req["store"] = false
	req["stream"] = true

	for _, key := range []string{
		"max_output_tokens",
		"max_completion_tokens",
		"temperature",
		"top_p",
		"frequency_penalty",
		"presence_penalty",
	} {
		delete(req, key)
	}

	switch input := req["input"].(type) {
	case string:
		req["input"] = []any{newUserMessage(strings.TrimSpace(input))}
	case []any:
		req["input"] = normalizeInputItems(input)
	case nil:
		req["input"] = []any{}
	}

	return json.Marshal(req)
}

func NewSessionID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "local-session"
	}
	return hex.EncodeToString(buf[:])
}

func normalizeInputItems(items []any) []any {
	if len(items) == 0 {
		return []any{}
	}

	if parts, ok := normalizeStandaloneInputParts(items); ok {
		return []any{
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": parts,
			},
		}
	}

	normalized := make([]any, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			normalized = append(normalized, item)
			continue
		}
		normalized = append(normalized, normalizeMessageItem(m))
	}
	return normalized
}

func normalizeStandaloneInputParts(items []any) ([]any, bool) {
	parts := make([]any, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		part, ok := normalizeContentPart(m)
		if !ok {
			return nil, false
		}
		parts = append(parts, part)
	}
	return parts, true
}

func normalizeMessageItem(item map[string]any) map[string]any {
	out := cloneMap(item)
	if role, ok := out["role"].(string); ok && strings.TrimSpace(role) != "" {
		out["type"] = "message"
		switch content := out["content"].(type) {
		case string:
			out["content"] = []any{map[string]any{
				"type": "input_text",
				"text": content,
			}}
		case []any:
			out["content"] = normalizeContentParts(content)
		}
	}
	return out
}

func normalizeContentParts(items []any) []any {
	parts := make([]any, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			parts = append(parts, item)
			continue
		}
		if part, ok := normalizeContentPart(m); ok {
			parts = append(parts, part)
			continue
		}
		parts = append(parts, item)
	}
	return parts
}

func normalizeContentPart(item map[string]any) (map[string]any, bool) {
	out := cloneMap(item)
	switch strings.TrimSpace(strings.ToLower(stringValue(item["type"]))) {
	case "text", "input_text":
		out["type"] = "input_text"
		return out, true
	case "image_url", "input_image":
		out["type"] = "input_image"
		return out, true
	default:
		return nil, false
	}
}

func newUserMessage(text string) map[string]any {
	if text == "" {
		return map[string]any{
			"type":    "message",
			"role":    "user",
			"content": []any{},
		}
	}
	return map[string]any{
		"type": "message",
		"role": "user",
		"content": []any{
			map[string]any{
				"type": "input_text",
				"text": text,
			},
		},
	}
}

func normalizeCodexModel(model string) string {
	switch strings.TrimSpace(strings.ToLower(model)) {
	case "", "gpt-5", "gpt-5-mini", "gpt-5-nano":
		return "gpt-5.1"
	case "gpt-5-codex", "gpt-5.1-codex":
		return "gpt-5.1-codex"
	case "gpt-5.1-codex-mini", "codex-mini-latest", "gpt-5-codex-mini":
		return "gpt-5.1-codex-mini"
	case "gpt-5.1-codex-max":
		return "gpt-5.1-codex-max"
	case "gpt-5.2-codex":
		return "gpt-5.2-codex"
	case "gpt-5.2":
		return "gpt-5.2"
	case "gpt-5.3", "gpt-5.3-codex", "gpt-5.3-codex-spark":
		return "gpt-5.3-codex"
	case "gpt-5.4":
		return "gpt-5.4"
	default:
		return model
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

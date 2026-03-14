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

type ResponsesStateDiagnostics struct {
	PreviousResponseID string
	ReferencedItemIDs  []string
	ItemReferenceCount int
	SystemMessageCount int
	DeveloperMessageCount int
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

	req["store"] = false
	req["stream"] = true
	delete(req, "previous_response_id")

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
		normalizedInput, extractedInstructions := normalizeInputItems(input)
		req["input"] = normalizedInput
		req["instructions"] = mergedInstructions(req["instructions"], extractedInstructions)
	case nil:
		req["input"] = []any{}
	}
	if _, ok := req["instructions"]; !ok {
		req["instructions"] = defaultInstructions
	} else if instructions, ok := req["instructions"].(string); ok && strings.TrimSpace(instructions) == "" {
		req["instructions"] = defaultInstructions
	}

	return json.Marshal(req)
}

func StripPreviousResponseID(body []byte) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	delete(req, "previous_response_id")
	return json.Marshal(req)
}

func InspectResponsesState(body []byte) (ResponsesStateDiagnostics, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return ResponsesStateDiagnostics{}, err
	}

	var diag ResponsesStateDiagnostics
	if previous, ok := req["previous_response_id"].(string); ok {
		diag.PreviousResponseID = strings.TrimSpace(previous)
	}
	if input, ok := req["input"].([]any); ok {
		inspectInputState(input, &diag)
	}
	return diag, nil
}

func NewSessionID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "local-session"
	}
	return hex.EncodeToString(buf[:])
}

func normalizeInputItems(items []any) ([]any, string) {
	if len(items) == 0 {
		return []any{}, ""
	}

	if parts, ok := normalizeStandaloneInputParts(items); ok {
		return []any{
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": parts,
			},
		}, ""
	}

	normalized := make([]any, 0, len(items))
	var instructions []string
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			normalized = append(normalized, item)
			continue
		}
		itemType := strings.TrimSpace(strings.ToLower(stringValue(m["type"])))
		if itemType == "item_reference" || itemType == "reference" {
			continue
		}
		role := strings.TrimSpace(strings.ToLower(stringValue(m["role"])))
		if role == "system" || role == "developer" {
			if text := instructionText(m["content"]); text != "" {
				instructions = append(instructions, text)
			}
			continue
		}
		normalized = append(normalized, stripReferencedIDs(normalizeMessageItem(m)))
	}
	return normalized, strings.Join(instructions, "\n\n")
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

func mergedInstructions(existing any, extracted string) string {
	base := strings.TrimSpace(stringValue(existing))
	extracted = strings.TrimSpace(extracted)
	switch {
	case base == "":
		return extracted
	case extracted == "":
		return base
	default:
		return base + "\n\n" + extracted
	}
}

func instructionText(content any) string {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			partType := strings.TrimSpace(strings.ToLower(stringValue(m["type"])))
			if partType != "" && partType != "text" && partType != "input_text" {
				continue
			}
			text := strings.TrimSpace(stringValue(m["text"]))
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func stripReferencedIDs(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			if key == "id" {
				if id, ok := child.(string); ok && looksLikeStoredItemID(id) {
					continue
				}
			}
			out[key] = stripReferencedIDs(child)
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, child := range v {
			out = append(out, stripReferencedIDs(child))
		}
		return out
	default:
		return value
	}
}

func looksLikeStoredItemID(id string) bool {
	id = strings.TrimSpace(id)
	return strings.HasPrefix(id, "rs_") || strings.HasPrefix(id, "resp_") || strings.HasPrefix(id, "msg_")
}

func inspectInputState(items []any, diag *ResponsesStateDiagnostics) {
	for _, item := range items {
		inspectStateValue(item, diag)
	}
}

func inspectStateValue(value any, diag *ResponsesStateDiagnostics) {
	switch v := value.(type) {
	case map[string]any:
		itemType := strings.TrimSpace(strings.ToLower(stringValue(v["type"])))
		if itemType == "item_reference" || itemType == "reference" {
			diag.ItemReferenceCount++
		}
		role := strings.TrimSpace(strings.ToLower(stringValue(v["role"])))
		switch role {
		case "system":
			diag.SystemMessageCount++
		case "developer":
			diag.DeveloperMessageCount++
		}
		for key, child := range v {
			if key == "id" {
				if id, ok := child.(string); ok && looksLikeStoredItemID(id) {
					diag.ReferencedItemIDs = append(diag.ReferencedItemIDs, strings.TrimSpace(id))
				}
			}
			inspectStateValue(child, diag)
		}
	case []any:
		for _, child := range v {
			inspectStateValue(child, diag)
		}
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

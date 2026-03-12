package openai

import (
	"encoding/json"
	"testing"
)

func TestNormalizeCodexResponsesRequest_NormalizesStringInput(t *testing.T) {
	body, err := NormalizeCodexResponsesRequest([]byte(`{"model":"gpt-5.3-codex-spark","input":"hello"}`))
	if err != nil {
		t.Fatalf("NormalizeCodexResponsesRequest returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}

	if got := payload["model"]; got != "gpt-5.3-codex" {
		t.Fatalf("unexpected normalized model: %#v", got)
	}
	if got, _ := payload["store"].(bool); got {
		t.Fatalf("expected store=false, got true")
	}
	if got, _ := payload["stream"].(bool); !got {
		t.Fatalf("expected stream=true, got false")
	}
	if instructions, _ := payload["instructions"].(string); instructions == "" {
		t.Fatalf("expected default instructions, got %#v", payload["instructions"])
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("expected normalized input array, got %#v", payload["input"])
	}
	first, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("expected message object, got %#v", input[0])
	}
	if first["type"] != "message" || first["role"] != "user" {
		t.Fatalf("unexpected first item: %#v", first)
	}
}

func TestNormalizeCodexResponsesRequest_NormalizesLooseTextParts(t *testing.T) {
	body, err := NormalizeCodexResponsesRequest([]byte(`{"model":"gpt-5.1-codex","input":[{"type":"text","text":"hi"}]}`))
	if err != nil {
		t.Fatalf("NormalizeCodexResponsesRequest returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("expected normalized message array, got %#v", payload["input"])
	}
	first, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first message item to be object, got %#v", input[0])
	}
	content, ok := first["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("expected normalized content parts, got %#v", first["content"])
	}
	part, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first content part to be object, got %#v", content[0])
	}
	if part["type"] != "input_text" || part["text"] != "hi" {
		t.Fatalf("unexpected normalized content part: %#v", part)
	}
}

func TestConvertChatCompletionsToResponses_MovesSystemMessagesToInstructions(t *testing.T) {
	body, _, err := ConvertChatCompletionsToResponses([]byte(`{
		"model":"gpt-5.1-codex",
		"messages":[
			{"role":"system","content":"You are terse."},
			{"role":"user","content":"Say OK"}
		]
	}`))
	if err != nil {
		t.Fatalf("ConvertChatCompletionsToResponses returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal converted body: %v", err)
	}

	if got, _ := payload["instructions"].(string); got != "You are terse." {
		t.Fatalf("expected system message to become instructions, got %#v", payload["instructions"])
	}

	input, ok := payload["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("expected only user message in input, got %#v", payload["input"])
	}
	first, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first input item to be object, got %#v", input[0])
	}
	if first["role"] != "user" {
		t.Fatalf("expected first input role to be user, got %#v", first["role"])
	}
}

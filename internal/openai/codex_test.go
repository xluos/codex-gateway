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

func TestNormalizeCodexResponsesRequest_StripsPreviousResponseID(t *testing.T) {
	body, err := NormalizeCodexResponsesRequest([]byte(`{"model":"gpt-5.1-codex","input":"hi","previous_response_id":"resp_123"}`))
	if err != nil {
		t.Fatalf("NormalizeCodexResponsesRequest returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}
	if _, ok := payload["previous_response_id"]; ok {
		t.Fatalf("expected previous_response_id to be removed, got %#v", payload["previous_response_id"])
	}
}

func TestNormalizeCodexResponsesRequest_MovesSystemInputToInstructions(t *testing.T) {
	body, err := NormalizeCodexResponsesRequest([]byte(`{
		"model":"gpt-5.1-codex",
		"input":[
			{"role":"system","content":"You are terse."},
			{"role":"user","content":"Say OK"}
		]
	}`))
	if err != nil {
		t.Fatalf("NormalizeCodexResponsesRequest returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}
	if got, _ := payload["instructions"].(string); got != "You are terse." {
		t.Fatalf("expected system message to become instructions, got %#v", payload["instructions"])
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("expected only non-system input items, got %#v", payload["input"])
	}
	first, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first input item to be object, got %#v", input[0])
	}
	if first["role"] != "user" {
		t.Fatalf("expected remaining input role to be user, got %#v", first["role"])
	}
}

func TestNormalizeCodexResponsesRequest_StripsReferencedInputIDs(t *testing.T) {
	body, err := NormalizeCodexResponsesRequest([]byte(`{
		"model":"gpt-5.1-codex",
		"input":[
			{"id":"rs_abc123","role":"user","content":"Say OK"},
			{"type":"item_reference","id":"rs_old"}
		]
	}`))
	if err != nil {
		t.Fatalf("NormalizeCodexResponsesRequest returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal normalized body: %v", err)
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("expected item references to be removed, got %#v", payload["input"])
	}
	first, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first input item to be object, got %#v", input[0])
	}
	if _, ok := first["id"]; ok {
		t.Fatalf("expected referenced input id to be removed, got %#v", first["id"])
	}
}

func TestInspectResponsesState_FindsStateReferences(t *testing.T) {
	diag, err := InspectResponsesState([]byte(`{
		"model":"gpt-5.1-codex",
		"previous_response_id":"resp_prev",
		"input":[
			{"id":"rs_abc123","role":"system","content":"Be terse"},
			{"type":"item_reference","id":"rs_old"},
			{"role":"developer","content":[{"type":"text","text":"Use tests"}]}
		]
	}`))
	if err != nil {
		t.Fatalf("InspectResponsesState returned error: %v", err)
	}
	if diag.PreviousResponseID != "resp_prev" {
		t.Fatalf("unexpected previous_response_id: %#v", diag.PreviousResponseID)
	}
	if len(diag.ReferencedItemIDs) != 2 {
		t.Fatalf("expected two referenced item ids, got %#v", diag.ReferencedItemIDs)
	}
	if diag.ItemReferenceCount != 1 {
		t.Fatalf("expected one item_reference, got %d", diag.ItemReferenceCount)
	}
	if diag.SystemMessageCount != 1 {
		t.Fatalf("expected one system message, got %d", diag.SystemMessageCount)
	}
	if diag.DeveloperMessageCount != 1 {
		t.Fatalf("expected one developer message, got %d", diag.DeveloperMessageCount)
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

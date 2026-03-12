package openai

import "testing"

func TestValidateJSONRequest_RejectsMissingModel(t *testing.T) {
	err := ValidateJSONRequest([]byte(`{"stream":true}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateJSONRequest_RejectsInvalidStreamType(t *testing.T) {
	err := ValidateJSONRequest([]byte(`{"model":"gpt-4.1","stream":"yes"}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateResponsesRequest_RejectsMessageIDAsPreviousResponseID(t *testing.T) {
	err := ValidateResponsesRequest([]byte(`{"model":"gpt-4.1","previous_response_id":"msg_123"}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateResponsesRequest_AllowsResponseID(t *testing.T) {
	err := ValidateResponsesRequest([]byte(`{"model":"gpt-4.1","previous_response_id":"resp_123"}`))
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

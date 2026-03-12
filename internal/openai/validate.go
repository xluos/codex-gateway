package openai

import (
	"encoding/json"
	"errors"
	"strings"
)

func ValidateJSONRequest(body []byte) error {
	if len(body) == 0 {
		return errors.New("request body is empty")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return errors.New("failed to parse request body")
	}

	modelRaw, ok := raw["model"]
	if !ok {
		return errors.New("model is required")
	}

	var model string
	if err := json.Unmarshal(modelRaw, &model); err != nil || strings.TrimSpace(model) == "" {
		return errors.New("model is required")
	}

	if streamRaw, ok := raw["stream"]; ok {
		var stream bool
		if err := json.Unmarshal(streamRaw, &stream); err != nil {
			return errors.New("invalid stream field type")
		}
	}

	return nil
}

func ValidateResponsesRequest(body []byte) error {
	if err := ValidateJSONRequest(body); err != nil {
		return err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return errors.New("failed to parse request body")
	}

	if previousRaw, ok := raw["previous_response_id"]; ok {
		var previous string
		if err := json.Unmarshal(previousRaw, &previous); err == nil {
			if strings.HasPrefix(strings.TrimSpace(previous), "msg_") {
				return errors.New("previous_response_id must be a response.id")
			}
		}
	}

	return nil
}

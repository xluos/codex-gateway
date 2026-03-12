package openai

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ChatCompletionsRequest struct {
	Model               string             `json:"model"`
	Messages            []ChatMessage      `json:"messages"`
	MaxTokens           *int               `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int               `json:"max_completion_tokens,omitempty"`
	Stream              bool               `json:"stream,omitempty"`
	StreamOptions       *ChatStreamOptions `json:"stream_options,omitempty"`
	ReasoningEffort     string             `json:"reasoning_effort,omitempty"`
	ServiceTier         string             `json:"service_tier,omitempty"`
}

type ChatStreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type ChatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type ChatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *ChatImageURL `json:"image_url,omitempty"`
}

type ChatImageURL struct {
	URL string `json:"url"`
}

type ChatCompletionsResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   *ChatUsage   `json:"usage,omitempty"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatCompletionsChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []ChatChunkChoice `json:"choices"`
	Usage   *ChatUsage        `json:"usage,omitempty"`
}

type ChatChunkChoice struct {
	Index        int       `json:"index"`
	Delta        ChatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

type ChatDelta struct {
	Role    string  `json:"role,omitempty"`
	Content *string `json:"content,omitempty"`
}

type ChatRequestMeta struct {
	OriginalModel string
	ClientStream  bool
	IncludeUsage  bool
}

type codexStreamEvent struct {
	Type     string            `json:"type"`
	Response *codexResponse    `json:"response,omitempty"`
	Delta    string            `json:"delta,omitempty"`
	Output   []codexOutputItem `json:"output,omitempty"`
}

type codexResponse struct {
	ID                string            `json:"id"`
	Model             string            `json:"model"`
	Status            string            `json:"status"`
	IncompleteDetails *codexIncomplete  `json:"incomplete_details,omitempty"`
	Usage             *codexUsage       `json:"usage,omitempty"`
	Output            []codexOutputItem `json:"output,omitempty"`
}

type codexIncomplete struct {
	Reason string `json:"reason"`
}

type codexUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type codexOutputItem struct {
	Type    string             `json:"type"`
	Role    string             `json:"role,omitempty"`
	Content []codexContentPart `json:"content,omitempty"`
}

type codexContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type chatStreamState struct {
	ID           string
	Model        string
	Created      int64
	SentRole     bool
	Finalized    bool
	IncludeUsage bool
	Usage        *ChatUsage
}

func ConvertChatCompletionsToResponses(body []byte) ([]byte, ChatRequestMeta, error) {
	var req ChatCompletionsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, ChatRequestMeta{}, fmt.Errorf("failed to parse request body")
	}

	resp := map[string]any{
		"model": normalizeCodexModel(req.Model),
	}
	input, instructions := convertChatMessages(req.Messages)
	resp["input"] = input
	if instructions != "" {
		resp["instructions"] = instructions
	}
	if req.ReasoningEffort != "" {
		resp["reasoning"] = map[string]any{
			"effort":  req.ReasoningEffort,
			"summary": "auto",
		}
	}
	if req.ServiceTier != "" {
		resp["service_tier"] = req.ServiceTier
	}
	if maxTokens := preferredMaxTokens(req.MaxTokens, req.MaxCompletionTokens); maxTokens != nil {
		resp["max_output_tokens"] = *maxTokens
	}

	normalized, err := NormalizeCodexResponsesRequest(mustJSON(resp))
	if err != nil {
		return nil, ChatRequestMeta{}, err
	}

	return normalized, ChatRequestMeta{
		OriginalModel: req.Model,
		ClientStream:  req.Stream,
		IncludeUsage:  req.StreamOptions != nil && req.StreamOptions.IncludeUsage,
	}, nil
}

func WriteChatCompletionsJSON(w http.ResponseWriter, body io.Reader, originalModel string) error {
	response, err := readChatCompletionFromStream(body, originalModel)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(response)
}

func WriteChatCompletionsStream(w http.ResponseWriter, body io.Reader, originalModel string, includeUsage bool) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, _ := w.(http.Flusher)
	state := chatStreamState{
		ID:           "chatcmpl-local",
		Model:        originalModel,
		Created:      time.Now().Unix(),
		IncludeUsage: includeUsage,
	}

	scanner := newSSEScanner(body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" || payload == "" {
			continue
		}

		var evt codexStreamEvent
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue
		}
		for _, chunk := range streamChunksFromEvent(evt, &state) {
			data, err := json.Marshal(chunk)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return err
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !state.Finalized {
		for _, chunk := range finalizeChatStream(&state) {
			data, err := json.Marshal(chunk)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return err
			}
		}
	}
	_, err := io.WriteString(w, "data: [DONE]\n\n")
	return err
}

func convertChatMessages(messages []ChatMessage) ([]any, string) {
	items := make([]any, 0, len(messages))
	var instructions []string
	for _, message := range messages {
		role := strings.TrimSpace(strings.ToLower(message.Role))
		switch role {
		case "system":
			if text := strings.TrimSpace(decodeChatInstruction(message.Content)); text != "" {
				instructions = append(instructions, text)
			}
			continue
		case "tool", "function":
			items = append(items, map[string]any{
				"type":    "function_call_output",
				"call_id": firstChatValue(message.ToolCallID, message.Name),
				"output":  decodeChatText(message.Content),
			})
		default:
			items = append(items, map[string]any{
				"type":    "message",
				"role":    normalizedChatRole(message.Role),
				"content": convertChatContent(message.Role, message.Content),
			})
		}
	}
	return items, strings.Join(instructions, "\n\n")
}

func convertChatContent(role string, raw json.RawMessage) []any {
	if len(raw) == 0 {
		return []any{}
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []any{newChatPart(role, "text", text)}
	}

	var parts []ChatContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return []any{newChatPart(role, "text", string(raw))}
	}

	converted := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "image_url":
			if part.ImageURL != nil && strings.TrimSpace(part.ImageURL.URL) != "" {
				converted = append(converted, map[string]any{
					"type":      "input_image",
					"image_url": part.ImageURL.URL,
				})
			}
		default:
			converted = append(converted, newChatPart(role, "text", part.Text))
		}
	}
	return converted
}

func newChatPart(role, kind, value string) map[string]any {
	partType := "input_text"
	if normalizedChatRole(role) == "assistant" {
		partType = "output_text"
	}
	if kind != "text" {
		partType = kind
	}
	return map[string]any{
		"type": partType,
		"text": value,
	}
}

func normalizedChatRole(role string) string {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "system", "assistant":
		return strings.TrimSpace(strings.ToLower(role))
	default:
		return "user"
	}
}

func decodeChatText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

func decodeChatInstruction(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []ChatContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return string(raw)
	}
	var builder strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part.Text) == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(part.Text)
	}
	return builder.String()
}

func preferredMaxTokens(maxTokens *int, maxCompletionTokens *int) *int {
	if maxCompletionTokens != nil {
		return maxCompletionTokens
	}
	return maxTokens
}

func mustJSON(v any) []byte {
	body, _ := json.Marshal(v)
	return body
}

func firstChatValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func readChatCompletionFromStream(body io.Reader, originalModel string) (*ChatCompletionsResponse, error) {
	response := &ChatCompletionsResponse{
		ID:      "chatcmpl-local",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   originalModel,
	}

	var content strings.Builder
	finishReason := "stop"

	scanner := newSSEScanner(body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" || payload == "" {
			continue
		}

		var evt codexStreamEvent
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue
		}
		switch evt.Type {
		case "response.created":
			if evt.Response != nil {
				if evt.Response.ID != "" {
					response.ID = evt.Response.ID
				}
				if evt.Response.Model != "" {
					response.Model = evt.Response.Model
				}
			}
		case "response.output_text.delta", "response.reasoning_summary_text.delta":
			content.WriteString(evt.Delta)
		case "response.completed", "response.incomplete", "response.failed":
			if evt.Response != nil {
				if evt.Response.ID != "" {
					response.ID = evt.Response.ID
				}
				if evt.Response.Model != "" {
					response.Model = evt.Response.Model
				}
				if content.Len() == 0 {
					content.WriteString(extractOutputText(evt.Response.Output))
				}
				if evt.Response.Usage != nil {
					response.Usage = &ChatUsage{
						PromptTokens:     evt.Response.Usage.InputTokens,
						CompletionTokens: evt.Response.Usage.OutputTokens,
						TotalTokens:      evt.Response.Usage.TotalTokens,
					}
				}
				if evt.Response.Status == "incomplete" && evt.Response.IncompleteDetails != nil && evt.Response.IncompleteDetails.Reason == "max_output_tokens" {
					finishReason = "length"
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	messageContent := content.String()
	messageContentJSON, _ := json.Marshal(messageContent)
	response.Choices = []ChatChoice{{
		Index: 0,
		Message: ChatMessage{
			Role:    "assistant",
			Content: messageContentJSON,
		},
		FinishReason: finishReason,
	}}
	return response, nil
}

func newSSEScanner(body io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	return scanner
}

func streamChunksFromEvent(evt codexStreamEvent, state *chatStreamState) []ChatCompletionsChunk {
	switch evt.Type {
	case "response.created":
		if evt.Response != nil {
			if evt.Response.ID != "" {
				state.ID = evt.Response.ID
			}
			if evt.Response.Model != "" {
				state.Model = evt.Response.Model
			}
		}
		if state.SentRole {
			return nil
		}
		state.SentRole = true
		return []ChatCompletionsChunk{makeRoleChunk(state)}
	case "response.output_text.delta", "response.reasoning_summary_text.delta":
		if !state.SentRole {
			state.SentRole = true
			return []ChatCompletionsChunk{makeRoleChunk(state), makeContentChunk(state, evt.Delta)}
		}
		return []ChatCompletionsChunk{makeContentChunk(state, evt.Delta)}
	case "response.completed", "response.incomplete", "response.failed":
		if evt.Response != nil && evt.Response.Usage != nil {
			state.Usage = &ChatUsage{
				PromptTokens:     evt.Response.Usage.InputTokens,
				CompletionTokens: evt.Response.Usage.OutputTokens,
				TotalTokens:      evt.Response.Usage.TotalTokens,
			}
		}
		if evt.Response != nil && evt.Response.Model != "" {
			state.Model = evt.Response.Model
		}
		finishReason := "stop"
		if evt.Response != nil && evt.Response.Status == "incomplete" && evt.Response.IncompleteDetails != nil && evt.Response.IncompleteDetails.Reason == "max_output_tokens" {
			finishReason = "length"
		}
		state.Finalized = true
		chunks := []ChatCompletionsChunk{makeFinishChunk(state, finishReason)}
		if state.IncludeUsage && state.Usage != nil {
			chunks = append(chunks, ChatCompletionsChunk{
				ID:      state.ID,
				Object:  "chat.completion.chunk",
				Created: state.Created,
				Model:   state.Model,
				Choices: []ChatChunkChoice{},
				Usage:   state.Usage,
			})
		}
		return chunks
	default:
		return nil
	}
}

func finalizeChatStream(state *chatStreamState) []ChatCompletionsChunk {
	state.Finalized = true
	chunks := []ChatCompletionsChunk{makeFinishChunk(state, "stop")}
	if state.IncludeUsage && state.Usage != nil {
		chunks = append(chunks, ChatCompletionsChunk{
			ID:      state.ID,
			Object:  "chat.completion.chunk",
			Created: state.Created,
			Model:   state.Model,
			Choices: []ChatChunkChoice{},
			Usage:   state.Usage,
		})
	}
	return chunks
}

func makeRoleChunk(state *chatStreamState) ChatCompletionsChunk {
	return ChatCompletionsChunk{
		ID:      state.ID,
		Object:  "chat.completion.chunk",
		Created: state.Created,
		Model:   state.Model,
		Choices: []ChatChunkChoice{{
			Index: 0,
			Delta: ChatDelta{Role: "assistant"},
		}},
	}
}

func makeContentChunk(state *chatStreamState, content string) ChatCompletionsChunk {
	return ChatCompletionsChunk{
		ID:      state.ID,
		Object:  "chat.completion.chunk",
		Created: state.Created,
		Model:   state.Model,
		Choices: []ChatChunkChoice{{
			Index: 0,
			Delta: ChatDelta{Content: &content},
		}},
	}
}

func makeFinishChunk(state *chatStreamState, finishReason string) ChatCompletionsChunk {
	return ChatCompletionsChunk{
		ID:      state.ID,
		Object:  "chat.completion.chunk",
		Created: state.Created,
		Model:   state.Model,
		Choices: []ChatChunkChoice{{
			Index:        0,
			Delta:        ChatDelta{},
			FinishReason: &finishReason,
		}},
	}
}

func extractOutputText(output []codexOutputItem) string {
	var builder strings.Builder
	for _, item := range output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" {
				builder.WriteString(part.Text)
			}
		}
	}
	return builder.String()
}

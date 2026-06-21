package converter

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/leeaandrob/claudex/internal/models"
)

// AnthropicToOpenAIRequest converts a native Anthropic Messages API request
// into the internal OpenAI-shaped request. This lets the Anthropic endpoint
// reuse the entire Claude CLI execution and tool-extraction pipeline; the
// response is converted back to Anthropic format with OpenAIToAnthropicResponse.
func (c *Converter) AnthropicToOpenAIRequest(req *models.AnthropicRequest) (*models.ChatCompletionRequest, error) {
	out := &models.ChatCompletionRequest{
		Model:     req.Model,
		Stream:    req.Stream,
		MaxTokens: req.MaxTokens,
	}

	// System prompt becomes a leading system message.
	if sys := req.SystemText(); sys != "" {
		out.Messages = append(out.Messages, models.Message{Role: "system", Content: sys})
	}

	for _, am := range req.Messages {
		blocks, err := am.Blocks()
		if err != nil {
			return nil, fmt.Errorf("invalid message content: %w", err)
		}

		msgs, err := anthropicBlocksToOpenAIMessages(am.Role, blocks)
		if err != nil {
			return nil, err
		}
		out.Messages = append(out.Messages, msgs...)
	}

	// Tools.
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, models.Tool{
			Type: "function",
			Function: models.Function{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	// Tool choice.
	if tc := anthropicToolChoice(req.ToolChoice); tc != nil {
		out.ToolChoice = tc
	}

	return out, nil
}

// anthropicBlocksToOpenAIMessages converts the content blocks of one Anthropic
// message into one or more OpenAI messages. tool_result blocks (which only
// appear on user turns) each become a separate "tool" message; tool_use blocks
// (assistant turns) become tool_calls; text/image blocks become the message
// content.
func anthropicBlocksToOpenAIMessages(role string, blocks []models.AnthropicContentBlock) ([]models.Message, error) {
	var out []models.Message
	var parts []models.ContentPart
	var toolCalls []models.ToolCall
	hasImage := false

	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, models.ContentPart{Type: "text", Text: b.Text})
		case "image":
			if b.Source != nil {
				url := fmt.Sprintf("data:%s;base64,%s", b.Source.MediaType, b.Source.Data)
				parts = append(parts, models.ContentPart{Type: "image_url", ImageURL: &models.ImageURL{URL: url}})
				hasImage = true
			}
		case "tool_use":
			args := "{}"
			if len(b.Input) > 0 {
				args = string(b.Input)
			}
			toolCalls = append(toolCalls, models.ToolCall{
				ID:       b.ID,
				Type:     "function",
				Function: models.FunctionCall{Name: b.Name, Arguments: args},
			})
		case "tool_result":
			// Each tool_result is emitted as its own OpenAI tool message.
			out = append(out, models.Message{
				Role:       "tool",
				ToolCallID: b.ToolUseID,
				Content:    b.TextContent(),
			})
		}
	}

	// Assemble the primary message (text/image content + any tool calls).
	if len(parts) > 0 || len(toolCalls) > 0 {
		msg := models.Message{Role: role, ToolCalls: toolCalls}
		switch {
		case hasImage:
			msg.Content = parts
		case len(parts) > 0:
			msg.Content = joinTextParts(parts)
		default:
			msg.Content = ""
		}
		out = append(out, msg)
	}

	return out, nil
}

// joinTextParts concatenates text parts into a single string (used when a
// message has no images and can be represented as plain text).
func joinTextParts(parts []models.ContentPart) string {
	var text string
	for _, p := range parts {
		if p.Type == "text" {
			text += p.Text
		}
	}
	return text
}

// anthropicToolChoice maps the Anthropic tool_choice object to the OpenAI
// tool_choice form. Returns nil when no usable choice is present.
func anthropicToolChoice(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var tc struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &tc); err != nil {
		return nil
	}
	switch tc.Type {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "none":
		return "none"
	case "tool":
		if tc.Name != "" {
			return models.ToolChoiceObject{
				Type:     "function",
				Function: models.ToolChoiceFunction{Name: tc.Name},
			}
		}
	}
	return nil
}

// OpenAIToAnthropicResponse converts an internal OpenAI-shaped response (the
// product of the Claude CLI pipeline) back into a native Anthropic Messages
// API response.
func (c *Converter) OpenAIToAnthropicResponse(resp *models.ChatCompletionResponse, model string) *models.AnthropicResponse {
	out := &models.AnthropicResponse{
		ID:           GenerateMessageID(),
		Type:         "message",
		Role:         "assistant",
		Model:        model,
		Content:      []models.AnthropicContentBlock{},
		StopReason:   "end_turn",
		StopSequence: nil,
	}

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]

		if text, ok := choice.Message.Content.(string); ok && text != "" {
			out.Content = append(out.Content, models.AnthropicContentBlock{Type: "text", Text: text})
		}

		for _, tc := range choice.Message.ToolCalls {
			out.Content = append(out.Content, models.AnthropicContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: argumentsToInput(tc.Function.Arguments),
			})
		}

		out.StopReason = openAIFinishToAnthropic(choice.FinishReason)
	}

	out.Usage = openAIUsageToAnthropic(resp.Usage)
	return out
}

// argumentsToInput turns an OpenAI tool-call arguments string (JSON-encoded)
// into the raw JSON object Anthropic expects in tool_use.input. Falls back to
// an empty object when the arguments are missing or invalid JSON.
func argumentsToInput(args string) json.RawMessage {
	if args == "" {
		return json.RawMessage("{}")
	}
	if json.Valid([]byte(args)) {
		return json.RawMessage(args)
	}
	return json.RawMessage("{}")
}

// openAIFinishToAnthropic maps an OpenAI finish_reason to an Anthropic
// stop_reason.
func openAIFinishToAnthropic(reason string) string {
	switch reason {
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return "end_turn"
	}
}

// openAIUsageToAnthropic maps the internal OpenAI usage shape back to the
// Anthropic usage shape. Anthropic's input_tokens excludes cached tokens, which
// are reported separately, so the cached portion is subtracted out.
func openAIUsageToAnthropic(u models.Usage) models.AnthropicUsage {
	cached := 0
	if u.PromptTokensDetails != nil {
		cached = u.PromptTokensDetails.CachedTokens
	}
	return models.AnthropicUsage{
		InputTokens:          u.PromptTokens - cached,
		OutputTokens:         u.CompletionTokens,
		CacheReadInputTokens: cached,
	}
}

// GenerateMessageID generates a unique message ID in Anthropic format.
func GenerateMessageID() string {
	return "msg_" + uuid.New().String()
}

// --- Streaming event builders (Anthropic SSE) ---------------------------------
//
// Each builder returns an SSEEvent (event name + payload). The handler writes
// them as `event: <Event>\ndata: <json(Data)>\n\n`.

// SSEEvent is a single Server-Sent Event: the event name and the payload to be
// JSON-encoded in the data field.
type SSEEvent struct {
	Event string
	Data  any
}

// AnthropicMessageStart builds the message_start event.
func (c *Converter) AnthropicMessageStart(id, model string, inputTokens int) SSEEvent {
	return SSEEvent{"message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            id,
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  inputTokens,
				"output_tokens": 0,
			},
		},
	}}
}

// AnthropicTextBlockStart builds a content_block_start for a text block.
func (c *Converter) AnthropicTextBlockStart(index int) SSEEvent {
	return SSEEvent{"content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         index,
		"content_block": map[string]any{"type": "text", "text": ""},
	}}
}

// AnthropicTextDelta builds a content_block_delta carrying a text fragment.
func (c *Converter) AnthropicTextDelta(index int, text string) SSEEvent {
	return SSEEvent{"content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]any{"type": "text_delta", "text": text},
	}}
}

// AnthropicToolUseBlockStart builds a content_block_start for a tool_use block.
func (c *Converter) AnthropicToolUseBlockStart(index int, id, name string) SSEEvent {
	return SSEEvent{"content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]any{
			"type":  "tool_use",
			"id":    id,
			"name":  name,
			"input": map[string]any{},
		},
	}}
}

// AnthropicInputJSONDelta builds a content_block_delta carrying tool input JSON.
func (c *Converter) AnthropicInputJSONDelta(index int, partialJSON string) SSEEvent {
	return SSEEvent{"content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": partialJSON},
	}}
}

// AnthropicBlockStop builds a content_block_stop event.
func (c *Converter) AnthropicBlockStop(index int) SSEEvent {
	return SSEEvent{"content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": index,
	}}
}

// AnthropicMessageDelta builds the message_delta event carrying the final
// stop_reason and output token count.
func (c *Converter) AnthropicMessageDelta(stopReason string, outputTokens int) SSEEvent {
	return SSEEvent{"message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{"output_tokens": outputTokens},
	}}
}

// AnthropicMessageStop builds the terminal message_stop event.
func (c *Converter) AnthropicMessageStop() SSEEvent {
	return SSEEvent{"message_stop", map[string]any{"type": "message_stop"}}
}

// AnthropicPing builds a ping keep-alive event.
func (c *Converter) AnthropicPing() SSEEvent {
	return SSEEvent{"ping", map[string]any{"type": "ping"}}
}

package models

import (
	"encoding/json"
	"strings"
)

// AnthropicRequest represents a native Anthropic Messages API request
// (POST /v1/messages). It mirrors the public Claude API request shape so
// callers can target claudex with the official Anthropic SDKs unchanged.
type AnthropicRequest struct {
	Model      string             `json:"model"`
	Messages   []AnthropicMessage `json:"messages"`
	System     json.RawMessage    `json:"system,omitempty"` // string | []AnthropicContentBlock
	MaxTokens  int                `json:"max_tokens,omitempty"`
	Stream     bool               `json:"stream,omitempty"`
	Tools      []AnthropicTool    `json:"tools,omitempty"`
	ToolChoice json.RawMessage    `json:"tool_choice,omitempty"` // {type: auto|any|tool, name?}
}

// AnthropicMessage is a single turn in the conversation. Content is either a
// plain string or an array of content blocks (text, image, tool_use,
// tool_result).
type AnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// AnthropicContentBlock is a content block used in both requests and
// responses. Fields are a superset across the block types; only those relevant
// to a given Type are populated.
type AnthropicContentBlock struct {
	Type string `json:"type"`

	// text
	Text string `json:"text,omitempty"`

	// image
	Source *AnthropicImageSource `json:"source,omitempty"`

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"` // string | []AnthropicContentBlock
	IsError   bool            `json:"is_error,omitempty"`
}

// AnthropicImageSource describes a base64-encoded image attached to a message.
type AnthropicImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", "image/jpeg", ...
	Data      string `json:"data"`       // base64-encoded bytes
}

// AnthropicTool is a tool definition in the Anthropic format.
type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// AnthropicResponse is a non-streaming Messages API response.
type AnthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"` // "message"
	Role         string                  `json:"role"` // "assistant"
	Model        string                  `json:"model"`
	Content      []AnthropicContentBlock `json:"content"`
	StopReason   string                  `json:"stop_reason"` // end_turn | tool_use | max_tokens
	StopSequence *string                 `json:"stop_sequence"`
	Usage        AnthropicUsage          `json:"usage"`
}

// AnthropicUsage reports token usage in the Anthropic shape.
type AnthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

// AnthropicErrorResponse is the Anthropic-format error envelope.
type AnthropicErrorResponse struct {
	Type  string               `json:"type"` // "error"
	Error AnthropicErrorDetail `json:"error"`
}

// AnthropicErrorDetail carries the error type and message.
type AnthropicErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// NewAnthropicError builds an Anthropic-format error envelope.
func NewAnthropicError(errType, message string) AnthropicErrorResponse {
	return AnthropicErrorResponse{
		Type:  "error",
		Error: AnthropicErrorDetail{Type: errType, Message: message},
	}
}

// SystemText extracts the system prompt as plain text. The Anthropic `system`
// field may be a string or an array of text blocks; both collapse to a single
// newline-joined string here.
func (r *AnthropicRequest) SystemText() string {
	if len(r.System) == 0 {
		return ""
	}

	// string form
	var s string
	if err := json.Unmarshal(r.System, &s); err == nil {
		return s
	}

	// array-of-blocks form
	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(r.System, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	return ""
}

// Blocks parses a message's content into content blocks. A bare string is
// normalized into a single text block so callers have a uniform view.
func (m *AnthropicMessage) Blocks() ([]AnthropicContentBlock, error) {
	if len(m.Content) == 0 {
		return nil, nil
	}

	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		if s == "" {
			return nil, nil
		}
		return []AnthropicContentBlock{{Type: "text", Text: s}}, nil
	}

	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

// TextContent extracts the plain text from a tool_result block's content,
// which may itself be a string or an array of text blocks.
func (b *AnthropicContentBlock) TextContent() string {
	if len(b.Content) == 0 {
		return b.Text
	}

	var s string
	if err := json.Unmarshal(b.Content, &s); err == nil {
		return s
	}

	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(b.Content, &blocks); err == nil {
		var parts []string
		for _, inner := range blocks {
			if inner.Type == "text" && inner.Text != "" {
				parts = append(parts, inner.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	return ""
}

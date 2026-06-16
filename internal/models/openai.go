package models

import (
	"encoding/json"
)

// ChatCompletionRequest represents an OpenAI-compatible chat completion request.
type ChatCompletionRequest struct {
	Model      string    `json:"model"`
	Messages   []Message `json:"messages"`
	Stream     bool      `json:"stream,omitempty"`
	Tools      []Tool    `json:"tools,omitempty"`
	ToolChoice any       `json:"tool_choice,omitempty"` // string | ToolChoiceObject
	MaxTokens  int       `json:"max_tokens,omitempty"`
}

// Tool represents an OpenAI function tool definition.
type Tool struct {
	Type     string   `json:"type"` // "function"
	Function Function `json:"function"`
}

// Function represents a function definition within a tool.
type Function struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall represents a tool call in an assistant message.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall represents the function being called.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ToolChoiceObject represents a specific tool choice.
type ToolChoiceObject struct {
	Type     string             `json:"type"` // "function"
	Function ToolChoiceFunction `json:"function"`
}

// ToolChoiceFunction specifies which function to use.
type ToolChoiceFunction struct {
	Name string `json:"name"`
}

// ContentPart represents a content block in multimodal format.
type ContentPart struct {
	Type     string    `json:"type"` // "text" | "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL represents an image URL in OpenAI format.
type ImageURL struct {
	URL    string `json:"url"`              // "data:image/png;base64,..." or URL
	Detail string `json:"detail,omitempty"` // "auto" | "low" | "high"
}

// Message represents a chat message with role and content.
// Content can be either a string or an array of ContentPart objects.
type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`                // string | []ContentPart
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // For assistant messages
	ToolCallID string     `json:"tool_call_id,omitempty"` // For tool result messages
}

// RawContent stores the raw JSON content for later processing.
type RawContent struct {
	Raw json.RawMessage
}

// messageAlias is used for unmarshaling to avoid recursion.
type messageAlias struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// UnmarshalJSON handles both string and array content formats.
func (m *Message) UnmarshalJSON(data []byte) error {
	var alias messageAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}

	m.Role = alias.Role
	m.ToolCalls = alias.ToolCalls
	m.ToolCallID = alias.ToolCallID

	// Handle null or empty content
	if len(alias.Content) == 0 || string(alias.Content) == "null" {
		m.Content = ""
		return nil
	}

	// Try to unmarshal as string first
	var contentStr string
	if err := json.Unmarshal(alias.Content, &contentStr); err == nil {
		m.Content = contentStr
		return nil
	}

	// Try to unmarshal as array of content parts
	var parts []ContentPart
	if err := json.Unmarshal(alias.Content, &parts); err != nil {
		// If both fail, store as raw for later processing
		m.Content = string(alias.Content)
		return nil
	}

	// Store the content parts
	m.Content = parts
	return nil
}

// MarshalJSON handles serialization of Message.
func (m Message) MarshalJSON() ([]byte, error) {
	type Alias struct {
		Role       string     `json:"role"`
		Content    any        `json:"content,omitempty"`
		ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
		ToolCallID string     `json:"tool_call_id,omitempty"`
	}

	alias := Alias{
		Role:       m.Role,
		Content:    m.Content,
		ToolCalls:  m.ToolCalls,
		ToolCallID: m.ToolCallID,
	}

	return json.Marshal(alias)
}

// GetTextContent extracts text content from a message.
// Works for both string content and content arrays.
func (m *Message) GetTextContent() string {
	switch c := m.Content.(type) {
	case string:
		return c
	case []ContentPart:
		var text string
		for _, part := range c {
			if part.Type == "text" {
				text += part.Text
			}
		}
		return text
	case []any:
		var text string
		for _, part := range c {
			if mp, ok := part.(map[string]any); ok {
				if mp["type"] == "text" {
					if t, ok := mp["text"].(string); ok {
						text += t
					}
				}
			}
		}
		return text
	}
	return ""
}

// HasImages checks if the message contains image content.
func (m *Message) HasImages() bool {
	switch c := m.Content.(type) {
	case []ContentPart:
		for _, part := range c {
			if part.Type == "image_url" {
				return true
			}
		}
	case []any:
		for _, part := range c {
			if mp, ok := part.(map[string]any); ok {
				if mp["type"] == "image_url" {
					return true
				}
			}
		}
	}
	return false
}

// ChatCompletionResponse represents a non-streaming chat completion response.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice represents a completion choice in a non-streaming response.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"` // "stop" | "tool_calls" | "length"
}

// Usage represents token usage statistics.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChunk represents a streaming chunk response.
type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
}

// ChunkChoice represents a choice in a streaming chunk.
type ChunkChoice struct {
	Index        int    `json:"index"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
}

// Delta represents incremental content in a streaming chunk.
type Delta struct {
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []ToolCallDelta `json:"tool_calls,omitempty"`
}

// ToolCallDelta represents incremental tool call data in streaming.
type ToolCallDelta struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function *FunctionCallDelta `json:"function,omitempty"`
}

// FunctionCallDelta represents incremental function call data.
type FunctionCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ErrorResponse represents an OpenAI-compatible error response.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error information.
type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

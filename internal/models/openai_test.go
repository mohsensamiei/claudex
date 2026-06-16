package models

import (
	"encoding/json"
	"testing"
)

func TestMessageUnmarshal_StringContent(t *testing.T) {
	input := `{"role": "user", "content": "Hello, world!"}`

	var msg Message
	err := json.Unmarshal([]byte(input), &msg)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if msg.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", msg.Role)
	}
	if msg.Content != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got '%s'", msg.Content)
	}
}

func TestMessageUnmarshal_ArrayContent(t *testing.T) {
	input := `{
		"role": "user",
		"content": [
			{"type": "text", "text": "What is in this image?"},
			{"type": "text", "text": " Please describe it."}
		]
	}`

	var msg Message
	err := json.Unmarshal([]byte(input), &msg)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if msg.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", msg.Role)
	}
	// Array content is preserved as []ContentPart (so images survive for
	// vision); GetTextContent joins the text parts.
	expected := "What is in this image? Please describe it."
	if got := msg.GetTextContent(); got != expected {
		t.Errorf("Expected content '%s', got '%s'", expected, got)
	}
}

func TestChatCompletionRequest_Basic(t *testing.T) {
	input := `{
		"model": "claude-3-sonnet",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Hello!"}
		],
		"stream": false
	}`

	var req ChatCompletionRequest
	err := json.Unmarshal([]byte(input), &req)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if req.Model != "claude-3-sonnet" {
		t.Errorf("Expected model 'claude-3-sonnet', got '%s'", req.Model)
	}
	if len(req.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(req.Messages))
	}
	if req.Stream != false {
		t.Errorf("Expected stream false, got %v", req.Stream)
	}
}

func TestChatCompletionResponse_Marshal(t *testing.T) {
	resp := ChatCompletionResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "claude-3-sonnet",
		Choices: []Choice{
			{
				Index: 0,
				Message: Message{
					Role:    "assistant",
					Content: "Hello! How can I help you?",
				},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     10,
			CompletionTokens: 8,
			TotalTokens:      18,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Verify it can be unmarshaled back
	var decoded ChatCompletionResponse
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.ID != resp.ID {
		t.Errorf("Expected ID '%s', got '%s'", resp.ID, decoded.ID)
	}
}

// ============================================================================
// TDD: Tests for Tool Calling (will fail until implemented)
// ============================================================================

func TestChatCompletionRequest_WithTools(t *testing.T) {
	t.Skip("TDD: Skipping until Tool Calling is implemented")

	input := `{
		"model": "claude-3-sonnet",
		"messages": [{"role": "user", "content": "Press the A button"}],
		"tools": [
			{
				"type": "function",
				"function": {
					"name": "press_buttons",
					"description": "Press buttons on the Game Boy",
					"parameters": {
						"type": "object",
						"properties": {
							"buttons": {
								"type": "array",
								"items": {"type": "string"}
							}
						},
						"required": ["buttons"]
					}
				}
			}
		],
		"tool_choice": "auto"
	}`

	var req ChatCompletionRequest
	err := json.Unmarshal([]byte(input), &req)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// These fields don't exist yet - TDD
	// if len(req.Tools) != 1 {
	// 	t.Errorf("Expected 1 tool, got %d", len(req.Tools))
	// }
	// if req.Tools[0].Function.Name != "press_buttons" {
	// 	t.Errorf("Expected tool name 'press_buttons'")
	// }
}

func TestChatCompletionResponse_WithToolCalls(t *testing.T) {
	t.Skip("TDD: Skipping until Tool Calling is implemented")

	// This tests the response format with tool_calls
	// Will be implemented when Tool Calling support is added
}

func TestMessage_WithToolResult(t *testing.T) {
	t.Skip("TDD: Skipping until Tool Calling is implemented")

	input := `{
		"role": "tool",
		"tool_call_id": "call_abc123",
		"content": "Button pressed successfully"
	}`

	var msg Message
	err := json.Unmarshal([]byte(input), &msg)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// These fields don't exist yet - TDD
	// if msg.Role != "tool" {
	// 	t.Errorf("Expected role 'tool', got '%s'", msg.Role)
	// }
	// if msg.ToolCallID != "call_abc123" {
	// 	t.Errorf("Expected tool_call_id 'call_abc123'")
	// }
}

// ============================================================================
// TDD: Tests for Vision/Images (will fail until implemented)
// ============================================================================

func TestMessageUnmarshal_WithImage(t *testing.T) {
	t.Skip("TDD: Skipping until Vision is implemented")

	input := `{
		"role": "user",
		"content": [
			{"type": "text", "text": "What do you see in this game?"},
			{
				"type": "image_url",
				"image_url": {
					"url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
				}
			}
		]
	}`

	var msg Message
	err := json.Unmarshal([]byte(input), &msg)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Vision support will need to preserve image data
	// Currently it only extracts text
}

func TestMessageUnmarshal_WithImageURL(t *testing.T) {
	t.Skip("TDD: Skipping until Vision is implemented")

	input := `{
		"role": "user",
		"content": [
			{"type": "text", "text": "Describe this screenshot"},
			{
				"type": "image_url",
				"image_url": {
					"url": "https://example.com/screenshot.png",
					"detail": "high"
				}
			}
		]
	}`

	var msg Message
	err := json.Unmarshal([]byte(input), &msg)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
}

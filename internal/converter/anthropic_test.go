package converter

import (
	"encoding/json"
	"testing"

	"github.com/leeaandrob/claudex/internal/models"
)

func TestAnthropicToOpenAIRequest_SystemAndText(t *testing.T) {
	c := NewConverter()
	req := &models.AnthropicRequest{
		Model:     "claude-sonnet",
		MaxTokens: 256,
		System:    json.RawMessage(`"You are helpful."`),
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
		},
	}

	out, err := c.AnthropicToOpenAIRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Model != "claude-sonnet" || out.MaxTokens != 256 {
		t.Fatalf("model/max_tokens not carried: %+v", out)
	}
	if len(out.Messages) != 2 {
		t.Fatalf("want 2 messages (system + user), got %d", len(out.Messages))
	}
	if out.Messages[0].Role != "system" || out.Messages[0].GetTextContent() != "You are helpful." {
		t.Fatalf("system message wrong: %+v", out.Messages[0])
	}
	if out.Messages[1].Role != "user" || out.Messages[1].GetTextContent() != "Hello" {
		t.Fatalf("user message wrong: %+v", out.Messages[1])
	}
}

func TestAnthropicToOpenAIRequest_ToolUseAndResult(t *testing.T) {
	c := NewConverter()
	req := &models.AnthropicRequest{
		Model: "claude-sonnet",
		Tools: []models.AnthropicTool{{
			Name:        "get_weather",
			Description: "Get weather",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
		ToolChoice: json.RawMessage(`{"type":"any"}`),
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: json.RawMessage(`"weather?"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Tokyo"}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_1","content":"sunny"}]`)},
		},
	}

	out, err := c.AnthropicToOpenAIRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(out.Tools) != 1 || out.Tools[0].Function.Name != "get_weather" {
		t.Fatalf("tool not converted: %+v", out.Tools)
	}
	if out.ToolChoice != "required" {
		t.Fatalf("tool_choice any should map to required, got %v", out.ToolChoice)
	}

	// Expect: user, assistant(tool_calls), tool(result)
	if len(out.Messages) != 3 {
		t.Fatalf("want 3 messages, got %d: %+v", len(out.Messages), out.Messages)
	}
	asst := out.Messages[1]
	if asst.Role != "assistant" || len(asst.ToolCalls) != 1 {
		t.Fatalf("assistant tool_calls missing: %+v", asst)
	}
	if asst.ToolCalls[0].Function.Name != "get_weather" || asst.ToolCalls[0].Function.Arguments != `{"city":"Tokyo"}` {
		t.Fatalf("tool call args wrong: %+v", asst.ToolCalls[0])
	}
	tool := out.Messages[2]
	if tool.Role != "tool" || tool.ToolCallID != "toolu_1" || tool.GetTextContent() != "sunny" {
		t.Fatalf("tool result wrong: %+v", tool)
	}
}

func TestAnthropicToOpenAIRequest_Image(t *testing.T) {
	c := NewConverter()
	req := &models.AnthropicRequest{
		Model: "claude-sonnet",
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"what is this"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]`)},
		},
	}
	out, err := c.AnthropicToOpenAIRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(out.Messages))
	}
	parts, ok := out.Messages[0].Content.([]models.ContentPart)
	if !ok {
		t.Fatalf("content should be []ContentPart, got %T", out.Messages[0].Content)
	}
	if len(parts) != 2 || parts[1].Type != "image_url" {
		t.Fatalf("image part missing: %+v", parts)
	}
	if parts[1].ImageURL == nil || parts[1].ImageURL.URL != "data:image/png;base64,AAAA" {
		t.Fatalf("image url wrong: %+v", parts[1].ImageURL)
	}
}

func TestOpenAIToAnthropicResponse_Text(t *testing.T) {
	c := NewConverter()
	resp := &models.ChatCompletionResponse{
		Choices: []models.Choice{{
			Message:      models.Message{Role: "assistant", Content: "Hi there"},
			FinishReason: "stop",
		}},
		Usage: models.Usage{
			PromptTokens:        100,
			CompletionTokens:    20,
			PromptTokensDetails: &models.PromptTokensDetails{CachedTokens: 80},
		},
	}

	out := c.OpenAIToAnthropicResponse(resp, "claude-sonnet")
	if out.Type != "message" || out.Role != "assistant" || out.Model != "claude-sonnet" {
		t.Fatalf("envelope wrong: %+v", out)
	}
	if len(out.Content) != 1 || out.Content[0].Type != "text" || out.Content[0].Text != "Hi there" {
		t.Fatalf("content wrong: %+v", out.Content)
	}
	if out.StopReason != "end_turn" {
		t.Fatalf("stop_reason should be end_turn, got %s", out.StopReason)
	}
	// input_tokens excludes the cached portion; cache reported separately.
	if out.Usage.InputTokens != 20 || out.Usage.OutputTokens != 20 || out.Usage.CacheReadInputTokens != 80 {
		t.Fatalf("usage wrong: %+v", out.Usage)
	}
}

func TestOpenAIToAnthropicResponse_ToolUse(t *testing.T) {
	c := NewConverter()
	resp := &models.ChatCompletionResponse{
		Choices: []models.Choice{{
			Message: models.Message{
				Role: "assistant",
				ToolCalls: []models.ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: models.FunctionCall{Name: "get_weather", Arguments: `{"city":"Tokyo"}`},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}

	out := c.OpenAIToAnthropicResponse(resp, "claude-sonnet")
	if out.StopReason != "tool_use" {
		t.Fatalf("stop_reason should be tool_use, got %s", out.StopReason)
	}
	if len(out.Content) != 1 || out.Content[0].Type != "tool_use" {
		t.Fatalf("tool_use block missing: %+v", out.Content)
	}
	blk := out.Content[0]
	if blk.ID != "call_1" || blk.Name != "get_weather" || string(blk.Input) != `{"city":"Tokyo"}` {
		t.Fatalf("tool_use block wrong: %+v", blk)
	}
}

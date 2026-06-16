package converter

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/leeaandrob/claudex/internal/models"
)

// Converter handles format conversion between OpenAI and Claude CLI.
type Converter struct{}

// NewConverter creates a new format converter.
func NewConverter() *Converter {
	return &Converter{}
}

// MessagesToPrompt converts OpenAI messages to Claude CLI prompt format.
// Returns the prompt and system prompt separately.
// This is used for the CLI backend (simple text requests).
func (c *Converter) MessagesToPrompt(messages []models.Message) (prompt, systemPrompt string) {
	var systemParts []string
	var conversationParts []string

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			systemParts = append(systemParts, msg.GetTextContent())
		case "user":
			conversationParts = append(conversationParts, "User: "+msg.GetTextContent())
		case "assistant":
			conversationParts = append(conversationParts, "Assistant: "+msg.GetTextContent())
		}
	}

	systemPrompt = strings.Join(systemParts, "\n")

	// For single user message, use directly without prefix
	// For conversation history, format as dialogue
	if len(conversationParts) == 1 && strings.HasPrefix(conversationParts[0], "User: ") {
		prompt = strings.TrimPrefix(conversationParts[0], "User: ")
	} else {
		prompt = strings.Join(conversationParts, "\n")
	}

	return prompt, systemPrompt
}

// NOTE: Anthropic API conversion methods removed as part of deprecation (PRP-002).
// All requests now use Claude CLI with stream-json format.

// ToolCallsResponse represents Claude's response when it wants to call tools.
type ToolCallsResponse struct {
	ToolCalls []ToolCallJSON `json:"tool_calls"`
}

// ToolCallJSON represents a tool call in Claude's JSON response.
type ToolCallJSON struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function FunctionCallJSON `json:"function"`
}

// FunctionCallJSON represents the function call details.
type FunctionCallJSON struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"` // Can be string or object
}

// GetArgumentsString returns arguments as a JSON string.
// Handles both string and object formats.
func (f *FunctionCallJSON) GetArgumentsString() string {
	if len(f.Arguments) == 0 {
		return "{}"
	}

	// Check if it's already a string (quoted)
	var str string
	if err := json.Unmarshal(f.Arguments, &str); err == nil {
		// It was a string, return it directly
		return str
	}

	// It's an object/array, return the raw JSON
	return string(f.Arguments)
}

// ClaudeToOpenAIResponse converts Claude JSON response to OpenAI format.
// This is used for CLI backend responses.
// It detects tool calls in Claude's response and properly formats them.
func (c *Converter) ClaudeToOpenAIResponse(claudeResp *models.ClaudeJSONResponse, model string) *models.ChatCompletionResponse {
	var content any = claudeResp.Result
	var toolCalls []models.ToolCall
	finishReason := "stop"

	// Try to extract tool calls from the response
	_, extractedToolCalls := c.ExtractToolCalls(claudeResp.Result)
	if len(extractedToolCalls) > 0 {
		toolCalls = extractedToolCalls
		finishReason = "tool_calls"
		// OpenAI spec: when finish_reason is "tool_calls" the assistant
		// message content is null (any accompanying prose is discarded).
		content = nil
	}

	return &models.ChatCompletionResponse{
		ID:      GenerateCompletionID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []models.Choice{
			{
				Index: 0,
				Message: models.Message{
					Role:      "assistant",
					Content:   content,
					ToolCalls: toolCalls,
				},
				FinishReason: finishReason,
			},
		},
		Usage: claudeUsageToOpenAI(claudeResp.Usage),
	}
}

// claudeUsageToOpenAI maps the Claude CLI usage report to OpenAI's usage shape.
//
// PromptTokens follows the OpenAI convention and includes both fresh input and
// cache tokens, so cost computed from it stays correct. The cache-read portion
// is surfaced separately via PromptTokensDetails.CachedTokens so callers can see
// that most of a large prompt_tokens value is the cached, fixed prefix (the
// Claude CLI system prompt + tool definitions) rather than their own input.
// Returns a zero-valued Usage when the CLI did not report usage.
func claudeUsageToOpenAI(u *models.ClaudeUsage) models.Usage {
	if u == nil {
		return models.Usage{}
	}

	promptTokens := u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	usage := models.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      promptTokens + u.OutputTokens,
	}
	if u.CacheReadInputTokens > 0 {
		usage.PromptTokensDetails = &models.PromptTokensDetails{
			CachedTokens: u.CacheReadInputTokens,
		}
	}
	return usage
}

// ExtractToolCalls attempts to extract tool calls from Claude's response text.
// Returns the remaining text content and any extracted tool calls.
func (c *Converter) ExtractToolCalls(content string) (string, []models.ToolCall) {
	content = strings.TrimSpace(content)

	// Try to find JSON with tool_calls in the response
	// Look for JSON block in markdown code fence
	jsonContent := c.extractJSONFromContent(content)
	if jsonContent == "" {
		return content, nil
	}

	// Try to parse as tool calls response
	var toolCallsResp ToolCallsResponse
	if err := json.Unmarshal([]byte(jsonContent), &toolCallsResp); err != nil {
		return content, nil
	}

	if len(toolCallsResp.ToolCalls) == 0 {
		return content, nil
	}

	// Convert to OpenAI format
	var toolCalls []models.ToolCall
	for _, tc := range toolCallsResp.ToolCalls {
		// Generate ID if not provided
		id := tc.ID
		if id == "" {
			id = GenerateToolCallID()
		}

		// Get arguments as string (handles both string and object formats)
		args := tc.Function.GetArgumentsString()

		toolCalls = append(toolCalls, models.ToolCall{
			ID:   id,
			Type: "function",
			Function: models.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: args,
			},
		})
	}

	// Extract text before/after the JSON block
	remainingText := c.extractTextOutsideJSON(content)

	return remainingText, toolCalls
}

// extractJSONFromContent extracts JSON from various formats (raw, code fence, etc.)
func (c *Converter) extractJSONFromContent(content string) string {
	// Try to find JSON in markdown code fence
	if idx := strings.Index(content, "```json"); idx != -1 {
		start := idx + 7
		end := strings.Index(content[start:], "```")
		if end != -1 {
			return strings.TrimSpace(content[start : start+end])
		}
	}

	// Try to find JSON in generic code fence
	if idx := strings.Index(content, "```"); idx != -1 {
		start := idx + 3
		// Skip optional language identifier
		if newline := strings.Index(content[start:], "\n"); newline != -1 {
			start += newline + 1
		}
		end := strings.Index(content[start:], "```")
		if end != -1 {
			jsonCandidate := strings.TrimSpace(content[start : start+end])
			if strings.HasPrefix(jsonCandidate, "{") {
				return jsonCandidate
			}
		}
	}

	// Try to find raw JSON starting with { and containing "tool_calls"
	if idx := strings.Index(content, `{"tool_calls"`); idx != -1 {
		// Find the matching closing brace
		jsonStr := c.extractJSONObject(content[idx:])
		if jsonStr != "" {
			return jsonStr
		}
	}

	// Try the entire content if it looks like JSON
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, "tool_calls") {
		return trimmed
	}

	return ""
}

// extractJSONObject extracts a complete JSON object starting from the current position.
func (c *Converter) extractJSONObject(content string) string {
	if !strings.HasPrefix(content, "{") {
		return ""
	}

	depth := 0
	inString := false
	escaped := false

	for i, char := range content {
		if escaped {
			escaped = false
			continue
		}

		if char == '\\' && inString {
			escaped = true
			continue
		}

		if char == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		if char == '{' {
			depth++
		} else if char == '}' {
			depth--
			if depth == 0 {
				return content[:i+1]
			}
		}
	}

	return ""
}

// extractTextOutsideJSON removes JSON blocks and returns remaining text.
func (c *Converter) extractTextOutsideJSON(content string) string {
	// Remove markdown code fences with JSON
	result := content

	// Remove ```json ... ``` blocks
	for {
		if idx := strings.Index(result, "```json"); idx != -1 {
			start := idx
			endMarker := strings.Index(result[idx+7:], "```")
			if endMarker != -1 {
				end := idx + 7 + endMarker + 3
				result = result[:start] + result[end:]
				continue
			}
		}
		break
	}

	// Remove ``` ... ``` blocks that contain tool_calls
	for {
		if idx := strings.Index(result, "```"); idx != -1 {
			endMarker := strings.Index(result[idx+3:], "```")
			if endMarker != -1 {
				blockContent := result[idx+3 : idx+3+endMarker]
				if strings.Contains(blockContent, "tool_calls") {
					end := idx + 3 + endMarker + 3
					result = result[:idx] + result[end:]
					continue
				}
			}
		}
		break
	}

	// Remove raw JSON objects with tool_calls
	if idx := strings.Index(result, `{"tool_calls"`); idx != -1 {
		jsonStr := c.extractJSONObject(result[idx:])
		if jsonStr != "" {
			result = result[:idx] + result[idx+len(jsonStr):]
		}
	}

	return strings.TrimSpace(result)
}

// ClaudeStreamToOpenAIChunk converts Claude streaming message to OpenAI chunk format.
// Note: Role is sent separately via CreateRoleChunk, so isFirst is unused but kept for API compatibility.
func (c *Converter) ClaudeStreamToOpenAIChunk(msg *models.ClaudeStreamMessage, id, model string, isFirst bool, prevContent string) (*models.ChatCompletionChunk, string) {
	chunk := &models.ChatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []models.ChunkChoice{
			{
				Index: 0,
				Delta: models.Delta{},
			},
		},
	}

	// Extract content delta from message
	var currentContent string
	if msg.Message != nil {
		currentContent = msg.Message.GetTextContent()
		// Calculate the delta (new content since last message)
		if len(currentContent) > len(prevContent) {
			chunk.Choices[0].Delta.Content = currentContent[len(prevContent):]
		}
	}

	return chunk, currentContent
}

// CreateRoleChunk creates the first streaming chunk with just the role.
func (c *Converter) CreateRoleChunk(id, model string) *models.ChatCompletionChunk {
	return &models.ChatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []models.ChunkChoice{
			{
				Index: 0,
				Delta: models.Delta{
					Role: "assistant",
				},
			},
		},
	}
}

// CreateContentChunk creates a streaming chunk with content delta.
func (c *Converter) CreateContentChunk(id, model, content string) *models.ChatCompletionChunk {
	return &models.ChatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []models.ChunkChoice{
			{
				Index: 0,
				Delta: models.Delta{
					Content: content,
				},
			},
		},
	}
}

// CreateToolCallChunk creates a streaming chunk with tool call delta.
func (c *Converter) CreateToolCallChunk(id, model string, toolIndex int, toolID, funcName, funcArgs string) *models.ChatCompletionChunk {
	chunk := &models.ChatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []models.ChunkChoice{
			{
				Index: 0,
				Delta: models.Delta{
					ToolCalls: []models.ToolCallDelta{
						{
							Index: toolIndex,
						},
					},
				},
			},
		},
	}

	if toolID != "" {
		chunk.Choices[0].Delta.ToolCalls[0].ID = toolID
		chunk.Choices[0].Delta.ToolCalls[0].Type = "function"
	}

	if funcName != "" || funcArgs != "" {
		chunk.Choices[0].Delta.ToolCalls[0].Function = &models.FunctionCallDelta{
			Name:      funcName,
			Arguments: funcArgs,
		}
	}

	return chunk
}

// CreateFinalChunk creates the final streaming chunk with finish_reason.
func (c *Converter) CreateFinalChunk(id, model string) *models.ChatCompletionChunk {
	return &models.ChatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []models.ChunkChoice{
			{
				Index:        0,
				Delta:        models.Delta{},
				FinishReason: "stop",
			},
		},
	}
}

// CreateToolCallFinalChunk creates the final streaming chunk for tool calls.
func (c *Converter) CreateToolCallFinalChunk(id, model string) *models.ChatCompletionChunk {
	return &models.ChatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []models.ChunkChoice{
			{
				Index:        0,
				Delta:        models.Delta{},
				FinishReason: "tool_calls",
			},
		},
	}
}

// GenerateCompletionID generates a unique completion ID in OpenAI format.
func GenerateCompletionID() string {
	return "chatcmpl-" + uuid.New().String()
}

// GenerateToolCallID generates a unique tool call ID.
func GenerateToolCallID() string {
	return "call_" + uuid.New().String()[:24]
}

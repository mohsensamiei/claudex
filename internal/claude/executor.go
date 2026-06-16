package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/leeaandrob/claudex/internal/models"
)

// Executor handles Claude CLI execution.
type Executor struct{}

// NewExecutor creates a new Claude CLI executor.
func NewExecutor() *Executor {
	return &Executor{}
}

// resolveModelFlag maps an OpenAI-style model identifier from the request to a
// value the Claude CLI accepts via --model. It returns an empty string when no
// model was requested, in which case the CLI default is used.
//
// The CLI accepts the short aliases "opus", "sonnet", and "haiku" as well as
// full model IDs. We normalize known family names to aliases and pass anything
// else through unchanged so future/explicit IDs still work.
func resolveModelFlag(model string) string {
	m := strings.TrimSpace(strings.ToLower(model))
	if m == "" {
		return ""
	}

	switch {
	case strings.Contains(m, "haiku"):
		return "haiku"
	case strings.Contains(m, "sonnet"):
		return "sonnet"
	case strings.Contains(m, "opus"):
		return "opus"
	default:
		// Unknown identifier: pass the original value through to the CLI.
		return model
	}
}

// appendModelFlag appends --model <value> to args when a model is requested.
func appendModelFlag(args []string, model string) []string {
	if flag := resolveModelFlag(model); flag != "" {
		args = append(args, "--model", flag)
	}
	return args
}

// StreamJSONMessage represents a message in stream-json input format.
type StreamJSONMessage struct {
	Type    string                `json:"type"`
	Message StreamJSONMessageBody `json:"message"`
}

// StreamJSONMessageBody represents the body of a stream-json message.
type StreamJSONMessageBody struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []StreamJSONContent
}

// StreamJSONContent represents a content block in stream-json format.
type StreamJSONContent struct {
	Type   string            `json:"type"` // "text" or "image"
	Text   string            `json:"text,omitempty"`
	Source *StreamJSONSource `json:"source,omitempty"`
}

// StreamJSONSource represents an image source in stream-json format.
type StreamJSONSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", "image/jpeg", etc.
	Data      string `json:"data"`       // base64 encoded data
}

// ExecuteWithMessages executes Claude CLI with OpenAI-style messages.
// Supports images and tools via stream-json input format.
func (e *Executor) ExecuteWithMessages(ctx context.Context, req *models.ChatCompletionRequest) (string, error) {
	// Build system prompt with tools if present
	systemPrompt := e.buildSystemPromptWithTools(req)

	// Check if we need stream-json input (for images or tools)
	hasImages := e.messagesHaveImages(req.Messages)
	hasTools := len(req.Tools) > 0

	// Use stream-json for images or tools, or when content is complex (arrays)
	useStreamJSON := hasImages || hasTools || e.messagesHaveComplexContent(req.Messages)

	if useStreamJSON {
		return e.executeWithStreamJSON(ctx, req.Messages, systemPrompt, req.Model, req.Stream)
	}

	// Simple text mode
	prompt := e.messagesToPrompt(req.Messages)
	if req.Stream {
		// For streaming, we return via the streaming method
		// This method is for non-streaming only
		return "", fmt.Errorf("use ExecuteStreamingWithMessages for streaming")
	}
	return e.ExecuteNonStreaming(ctx, prompt, systemPrompt, req.Model)
}

// messagesHaveComplexContent checks if any message has array content (potential images).
func (e *Executor) messagesHaveComplexContent(messages []models.Message) bool {
	for _, msg := range messages {
		switch msg.Content.(type) {
		case []any, []models.ContentPart:
			return true
		}
	}
	return false
}

// ExecuteStreamingWithMessages executes Claude CLI with streaming and OpenAI-style messages.
func (e *Executor) ExecuteStreamingWithMessages(ctx context.Context, req *models.ChatCompletionRequest) (<-chan string, <-chan error, error) {
	// Build system prompt with tools if present
	systemPrompt := e.buildSystemPromptWithTools(req)

	// Check if we need stream-json input (for images or tools)
	hasImages := e.messagesHaveImages(req.Messages)
	hasTools := len(req.Tools) > 0

	// Use stream-json for images or tools, or when content is complex (arrays)
	useStreamJSON := hasImages || hasTools || e.messagesHaveComplexContent(req.Messages)

	if useStreamJSON {
		return e.executeStreamingWithStreamJSON(ctx, req.Messages, systemPrompt, req.Model)
	}

	// Simple text mode
	prompt := e.messagesToPrompt(req.Messages)
	return e.ExecuteStreaming(ctx, prompt, systemPrompt, req.Model)
}

// executeWithStreamJSON executes using stream-json input format (for images).
func (e *Executor) executeWithStreamJSON(ctx context.Context, messages []models.Message, systemPrompt, model string, stream bool) (string, error) {
	// Note: stream-json input requires stream-json output, and --verbose is required with -p
	args := []string{"-p", "--verbose", "--input-format", "stream-json", "--output-format", "stream-json", "--dangerously-skip-permissions", "--no-chrome"}
	args = appendModelFlag(args, model)

	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)

	// Convert messages to stream-json format
	var inputLines []string
	for _, msg := range messages {
		if msg.Role == "system" {
			continue // System prompt handled separately
		}

		streamMsg := e.convertToStreamJSON(msg)
		jsonBytes, err := json.Marshal(streamMsg)
		if err != nil {
			return "", fmt.Errorf("failed to marshal message: %w", err)
		}
		inputLines = append(inputLines, string(jsonBytes))
	}

	// Join with newlines for NDJSON
	input := strings.Join(inputLines, "\n")
	cmd.Stdin = bytes.NewReader([]byte(input))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := stderr.String()
		if stderrStr != "" {
			return "", fmt.Errorf("claude cli error: %s", stderrStr)
		}
		return "", fmt.Errorf("claude cli error: %w", err)
	}

	// Parse stream-json output and extract the result
	return e.parseStreamJSONOutput(stdout.String())
}

// parseStreamJSONOutput extracts the final result from stream-json output lines.
func (e *Executor) parseStreamJSONOutput(output string) (string, error) {
	var resultText string

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)

		// Look for result event first (contains the final text)
		if eventType == "result" {
			if result, ok := event["result"].(string); ok {
				resultText = result
				break // Use the result event as authoritative
			}
		}
	}

	// If no result event, try to extract from assistant message
	if resultText == "" {
		for _, line := range lines {
			if line == "" {
				continue
			}

			var event map[string]any
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				continue
			}

			if event["type"] == "assistant" {
				if msg, ok := event["message"].(map[string]any); ok {
					if content, ok := msg["content"].(string); ok {
						resultText = content
						break
					} else if contentArr, ok := msg["content"].([]any); ok {
						var sb strings.Builder
						for _, c := range contentArr {
							if cMap, ok := c.(map[string]any); ok {
								if cMap["type"] == "text" {
									if text, ok := cMap["text"].(string); ok {
										sb.WriteString(text)
									}
								}
							}
						}
						resultText = sb.String()
						break
					}
				}
			}
		}
	}

	// Return as JSON format that the parser expects
	result := map[string]any{
		"type":   "result",
		"result": resultText,
	}

	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	return string(jsonBytes), nil
}

// executeStreamingWithStreamJSON executes streaming with stream-json input format.
func (e *Executor) executeStreamingWithStreamJSON(ctx context.Context, messages []models.Message, systemPrompt, model string) (<-chan string, <-chan error, error) {
	args := []string{"-p", "--verbose", "--input-format", "stream-json", "--output-format", "stream-json", "--include-partial-messages", "--dangerously-skip-permissions", "--no-chrome"}
	args = appendModelFlag(args, model)

	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)

	// Convert messages to stream-json format
	var inputLines []string
	for _, msg := range messages {
		if msg.Role == "system" {
			continue // System prompt handled separately
		}

		streamMsg := e.convertToStreamJSON(msg)
		jsonBytes, err := json.Marshal(streamMsg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal message: %w", err)
		}
		inputLines = append(inputLines, string(jsonBytes))
	}

	input := strings.Join(inputLines, "\n")
	cmd.Stdin = bytes.NewReader([]byte(input))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start claude cli: %w", err)
	}

	chunks := make(chan string, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errChan)

		var stderrBuf bytes.Buffer
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				stderrBuf.WriteString(scanner.Text())
				stderrBuf.WriteString("\n")
			}
		}()

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line != "" {
				chunks <- line
			}
		}

		if err := scanner.Err(); err != nil {
			errChan <- fmt.Errorf("scanner error: %w", err)
			return
		}

		if err := cmd.Wait(); err != nil {
			if stderrBuf.Len() > 0 {
				errChan <- fmt.Errorf("claude cli error: %s", stderrBuf.String())
			} else {
				errChan <- fmt.Errorf("claude cli error: %w", err)
			}
			return
		}
	}()

	return chunks, errChan, nil
}

// convertToStreamJSON converts an OpenAI message to stream-json format.
func (e *Executor) convertToStreamJSON(msg models.Message) StreamJSONMessage {
	streamMsg := StreamJSONMessage{
		Type: "user",
		Message: StreamJSONMessageBody{
			Role: msg.Role,
		},
	}

	// Map OpenAI roles to Claude roles
	if msg.Role == "assistant" {
		streamMsg.Type = "assistant"
	} else if msg.Role == "tool" {
		// Tool results are sent as user messages
		streamMsg.Type = "user"
		streamMsg.Message.Role = "user"
		// Include tool result as text
		streamMsg.Message.Content = fmt.Sprintf("[Tool Result for %s]: %s", msg.ToolCallID, msg.GetTextContent())
		return streamMsg
	}

	// Convert content
	switch c := msg.Content.(type) {
	case string:
		streamMsg.Message.Content = c
	case []models.ContentPart:
		streamMsg.Message.Content = e.convertContentParts(c)
	case []any:
		streamMsg.Message.Content = e.convertContentPartsFromAny(c)
	default:
		streamMsg.Message.Content = msg.GetTextContent()
	}

	return streamMsg
}

// convertContentParts converts OpenAI content parts to stream-json format.
func (e *Executor) convertContentParts(parts []models.ContentPart) []StreamJSONContent {
	var result []StreamJSONContent
	for _, part := range parts {
		switch part.Type {
		case "text":
			result = append(result, StreamJSONContent{
				Type: "text",
				Text: part.Text,
			})
		case "image_url":
			if img := e.convertImageURL(part.ImageURL); img != nil {
				result = append(result, *img)
			}
		}
	}
	return result
}

// convertContentPartsFromAny converts untyped content parts to stream-json format.
func (e *Executor) convertContentPartsFromAny(parts []any) []StreamJSONContent {
	var result []StreamJSONContent
	for _, part := range parts {
		if m, ok := part.(map[string]any); ok {
			partType, _ := m["type"].(string)
			switch partType {
			case "text":
				text, _ := m["text"].(string)
				result = append(result, StreamJSONContent{
					Type: "text",
					Text: text,
				})
			case "image_url":
				if imgData, ok := m["image_url"].(map[string]any); ok {
					url, _ := imgData["url"].(string)
					if img := e.convertImageURL(&models.ImageURL{URL: url}); img != nil {
						result = append(result, *img)
					}
				}
			}
		}
	}
	return result
}

// convertImageURL converts an OpenAI image_url to stream-json image format.
func (e *Executor) convertImageURL(imageURL *models.ImageURL) *StreamJSONContent {
	if imageURL == nil {
		return nil
	}

	url := imageURL.URL

	// Parse data URL: data:image/png;base64,xxxxx
	if strings.HasPrefix(url, "data:") {
		parts := strings.SplitN(url, ",", 2)
		if len(parts) != 2 {
			return nil
		}

		header := parts[0]
		mediaType := "image/png"

		if strings.Contains(header, "image/jpeg") || strings.Contains(header, "image/jpg") {
			mediaType = "image/jpeg"
		} else if strings.Contains(header, "image/webp") {
			mediaType = "image/webp"
		} else if strings.Contains(header, "image/gif") {
			mediaType = "image/gif"
		} else if strings.Contains(header, "image/png") {
			mediaType = "image/png"
		}

		return &StreamJSONContent{
			Type: "image",
			Source: &StreamJSONSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      parts[1],
			},
		}
	}

	return nil
}

// buildSystemPromptWithTools builds a system prompt that includes tool definitions.
func (e *Executor) buildSystemPromptWithTools(req *models.ChatCompletionRequest) string {
	var parts []string

	// Get system prompt from messages
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			parts = append(parts, msg.GetTextContent())
		}
	}

	// Add tool definitions if present
	if len(req.Tools) > 0 {
		toolsPrompt := e.buildToolsPrompt(req.Tools, req.ToolChoice)
		parts = append(parts, toolsPrompt)
	}

	return strings.Join(parts, "\n\n")
}

// buildToolsPrompt creates a prompt section describing available tools.
func (e *Executor) buildToolsPrompt(tools []models.Tool, toolChoice any) string {
	var sb strings.Builder

	sb.WriteString("## Available Tools\n\n")
	sb.WriteString("You have access to the following tools. When you decide to use a tool, you MUST respond with ONLY a JSON object (no other text before or after) in this exact format:\n\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"tool_calls\": [\n")
	sb.WriteString("    {\n")
	sb.WriteString("      \"id\": \"call_abc123\",\n")
	sb.WriteString("      \"type\": \"function\",\n")
	sb.WriteString("      \"function\": {\n")
	sb.WriteString("        \"name\": \"tool_name_here\",\n")
	sb.WriteString("        \"arguments\": \"{\\\"param1\\\": \\\"value1\\\"}\"\n")
	sb.WriteString("      }\n")
	sb.WriteString("    }\n")
	sb.WriteString("  ]\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")
	sb.WriteString("CRITICAL RULES:\n")
	sb.WriteString("1. The 'arguments' field MUST be a JSON-encoded STRING, not a raw object\n")
	sb.WriteString("2. Generate unique IDs like 'call_' followed by random alphanumeric characters\n")
	sb.WriteString("3. When using tools, output ONLY the JSON - no explanation text\n")
	sb.WriteString("4. You can include brief reasoning BEFORE the JSON if needed, but the JSON must be last\n\n")

	sb.WriteString("### Tool Definitions:\n\n")

	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		sb.WriteString(fmt.Sprintf("#### %s\n", tool.Function.Name))
		if tool.Function.Description != "" {
			sb.WriteString(fmt.Sprintf("Description: %s\n", tool.Function.Description))
		}
		if len(tool.Function.Parameters) > 0 {
			sb.WriteString(fmt.Sprintf("Parameters schema:\n```json\n%s\n```\n", string(tool.Function.Parameters)))
		}
		sb.WriteString("\n")
	}

	// Add tool_choice guidance
	if toolChoice != nil {
		switch v := toolChoice.(type) {
		case string:
			if v == "required" {
				sb.WriteString("\n**IMPORTANT**: You MUST use one of the available tools in your response. Do not respond with plain text only.\n")
			} else if v == "none" {
				sb.WriteString("\n**IMPORTANT**: Do NOT use any tools. Respond with plain text only.\n")
			} else if v == "auto" {
				sb.WriteString("\n**MODE**: Auto - Use tools when appropriate, or respond with text if no tool is needed.\n")
			}
		case map[string]any:
			if fn, ok := v["function"].(map[string]any); ok {
				if name, ok := fn["name"].(string); ok {
					sb.WriteString(fmt.Sprintf("\n**IMPORTANT**: You MUST use the '%s' tool in your response.\n", name))
				}
			}
		}
	}

	return sb.String()
}

// messagesHaveImages checks if any message contains images.
func (e *Executor) messagesHaveImages(messages []models.Message) bool {
	for _, msg := range messages {
		if msg.HasImages() {
			return true
		}
	}
	return false
}

// messagesToPrompt converts messages to a simple text prompt.
func (e *Executor) messagesToPrompt(messages []models.Message) string {
	var parts []string

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			// Skip, handled separately
		case "user":
			parts = append(parts, "User: "+msg.GetTextContent())
		case "assistant":
			parts = append(parts, "Assistant: "+msg.GetTextContent())
		case "tool":
			parts = append(parts, fmt.Sprintf("[Tool Result for %s]: %s", msg.ToolCallID, msg.GetTextContent()))
		}
	}

	if len(parts) == 1 && strings.HasPrefix(parts[0], "User: ") {
		return strings.TrimPrefix(parts[0], "User: ")
	}

	return strings.Join(parts, "\n")
}

// ExecuteNonStreaming executes Claude CLI and returns the complete response.
func (e *Executor) ExecuteNonStreaming(ctx context.Context, prompt, systemPrompt, model string) (string, error) {
	args := []string{"-p", "--output-format", "json", "--dangerously-skip-permissions", "--no-chrome"}
	args = appendModelFlag(args, model)

	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}
	args = append(args, "-")

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdin = bytes.NewReader([]byte(prompt))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := stderr.String()
		if stderrStr != "" {
			return "", fmt.Errorf("claude cli error: %s", stderrStr)
		}
		return "", fmt.Errorf("claude cli error: %w", err)
	}

	return stdout.String(), nil
}

// ExecuteStreaming executes Claude CLI with streaming output.
func (e *Executor) ExecuteStreaming(ctx context.Context, prompt, systemPrompt, model string) (<-chan string, <-chan error, error) {
	args := []string{"-p", "--verbose", "--output-format", "stream-json", "--include-partial-messages", "--dangerously-skip-permissions", "--no-chrome"}
	args = appendModelFlag(args, model)

	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}
	args = append(args, "-")

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdin = bytes.NewReader([]byte(prompt))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start claude cli: %w", err)
	}

	chunks := make(chan string, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errChan)

		var stderrBuf bytes.Buffer
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				stderrBuf.WriteString(scanner.Text())
				stderrBuf.WriteString("\n")
			}
		}()

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line != "" {
				chunks <- line
			}
		}

		if err := scanner.Err(); err != nil {
			errChan <- fmt.Errorf("scanner error: %w", err)
			return
		}

		if err := cmd.Wait(); err != nil {
			if stderrBuf.Len() > 0 {
				errChan <- fmt.Errorf("claude cli error: %s", stderrBuf.String())
			} else {
				errChan <- fmt.Errorf("claude cli error: %w", err)
			}
			return
		}
	}()

	return chunks, errChan, nil
}

// IsAvailable checks if the Claude CLI is available.
func (e *Executor) IsAvailable() bool {
	cmd := exec.Command("claude", "--version")
	return cmd.Run() == nil
}

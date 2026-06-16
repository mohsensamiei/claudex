package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/leeaandrob/claudex/internal/claude"
	"github.com/leeaandrob/claudex/internal/converter"
	"github.com/leeaandrob/claudex/internal/mcp"
	"github.com/leeaandrob/claudex/internal/models"
	"github.com/leeaandrob/claudex/internal/observability"
	"github.com/valyala/fasthttp"
)

// getRequestTimeout returns the request timeout from environment or default (10 minutes)
func getRequestTimeout() time.Duration {
	if val := os.Getenv("REQUEST_TIMEOUT"); val != "" {
		if seconds, err := strconv.Atoi(val); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return 10 * time.Minute
}

// ChatCompletionsHandler handles chat completion requests.
type ChatCompletionsHandler struct {
	executor   *claude.Executor
	parser     *claude.Parser
	converter  *converter.Converter
	mcpManager *mcp.Manager
	metrics    *observability.Metrics
	logger     *observability.Logger
}

// NewChatCompletionsHandler creates a new chat completions handler.
func NewChatCompletionsHandler(
	executor *claude.Executor,
	parser *claude.Parser,
	conv *converter.Converter,
	mcpManager *mcp.Manager,
	metrics *observability.Metrics,
	logger *observability.Logger,
) *ChatCompletionsHandler {
	return &ChatCompletionsHandler{
		executor:   executor,
		parser:     parser,
		converter:  conv,
		mcpManager: mcpManager,
		metrics:    metrics,
		logger:     logger,
	}
}

// Handle processes chat completion requests.
//
//	@Summary		Create chat completion
//	@Description	Creates an OpenAI-compatible chat completion, proxied to the Claude CLI. Supports streaming, tool calling, and vision.
//	@Tags			chat
//	@Accept			json
//	@Produce		json
//	@Param			request	body		models.ChatCompletionRequest	true	"Chat completion request"
//	@Success		200		{object}	models.ChatCompletionResponse
//	@Failure		400		{object}	models.ErrorResponse
//	@Failure		401		{object}	models.ErrorResponse
//	@Failure		500		{object}	models.ErrorResponse
//	@Security		BearerAuth
//	@Router			/v1/chat/completions [post]
func (h *ChatCompletionsHandler) Handle(c *fiber.Ctx) error {
	start := time.Now()
	h.metrics.IncrementActive()
	defer h.metrics.DecrementActive()

	// Parse request body
	var req models.ChatCompletionRequest
	if err := c.BodyParser(&req); err != nil {
		h.metrics.RecordError("parse_error")
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error: models.ErrorDetail{
				Message: "Invalid request body: " + err.Error(),
				Type:    "invalid_request_error",
				Code:    "invalid_json",
			},
		})
	}

	// Validate messages
	if len(req.Messages) == 0 {
		h.metrics.RecordError("validation_error")
		return c.Status(fiber.StatusBadRequest).JSON(models.ErrorResponse{
			Error: models.ErrorDetail{
				Message: "Messages array is required and cannot be empty",
				Type:    "invalid_request_error",
				Code:    "invalid_messages",
			},
		})
	}

	// Add MCP tools to the request if available
	if h.mcpManager != nil && h.mcpManager.HasTools() {
		mcpTools := h.mcpManager.GetToolsAsOpenAI()
		req.Tools = append(req.Tools, mcpTools...)
	}

	// Use CLI for all requests (Anthropic API deprecated)
	if req.Stream {
		return h.handleStreamingCLI(c, &req, start)
	}
	return h.handleNonStreamingCLI(c, &req, start)
}

// handleNonStreamingCLI handles non-streaming requests using CLI.
func (h *ChatCompletionsHandler) handleNonStreamingCLI(c *fiber.Ctx, req *models.ChatCompletionRequest, start time.Time) error {
	ctx, cancel := context.WithTimeout(c.Context(), getRequestTimeout())
	defer cancel()

	claudeStart := time.Now()

	// Execute Claude CLI with messages (supports images and tools via stream-json)
	output, err := h.executor.ExecuteWithMessages(ctx, req)
	if err != nil {
		h.logger.Error("claude execution failed", "error", err.Error(), "model", req.Model)
		h.metrics.RecordError("claude_error")
		h.metrics.RecordRequest("error", false, time.Since(start).Seconds())
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error: models.ErrorDetail{
				Message: "Failed to execute Claude: " + err.Error(),
				Type:    "server_error",
				Code:    "claude_error",
			},
		})
	}

	h.metrics.RecordClaudeDuration(time.Since(claudeStart).Seconds())

	// Parse Claude response
	claudeResp, err := h.parser.ParseJSONResponse(output)
	if err != nil {
		h.metrics.RecordError("parse_error")
		h.metrics.RecordRequest("error", false, time.Since(start).Seconds())
		return c.Status(fiber.StatusInternalServerError).JSON(models.ErrorResponse{
			Error: models.ErrorDetail{
				Message: "Failed to parse Claude response: " + err.Error(),
				Type:    "server_error",
				Code:    "parse_error",
			},
		})
	}

	// Convert to OpenAI format (handles tool calls in response)
	openaiResp := h.converter.ClaudeToOpenAIResponse(claudeResp, req.Model)

	// Execute MCP tools if there are tool calls and MCP manager is available
	if len(openaiResp.Choices) > 0 && len(openaiResp.Choices[0].Message.ToolCalls) > 0 && h.mcpManager != nil {
		openaiResp = h.executeMCPToolCalls(ctx, openaiResp, req)
	}

	h.metrics.RecordRequest("success", false, time.Since(start).Seconds())

	return c.JSON(openaiResp)
}

// executeMCPToolCalls executes tool calls via MCP and returns the results.
func (h *ChatCompletionsHandler) executeMCPToolCalls(ctx context.Context, resp *models.ChatCompletionResponse, req *models.ChatCompletionRequest) *models.ChatCompletionResponse {
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
		return resp
	}

	toolCalls := resp.Choices[0].Message.ToolCalls
	var toolResults []models.Message

	for _, tc := range toolCalls {
		h.logger.Info("checking MCP tool availability", "tool_name", tc.Function.Name)

		// Check if this is an MCP tool
		if !h.mcpManager.IsToolAvailable(tc.Function.Name) {
			// Not an MCP tool, skip (caller handles non-MCP tools)
			h.logger.Info("tool not available via MCP, skipping", "tool_name", tc.Function.Name)
			continue
		}

		h.logger.Info("executing MCP tool", "tool_name", tc.Function.Name, "arguments", tc.Function.Arguments)

		// Execute the tool via MCP
		result, err := h.mcpManager.CallTool(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
		if err != nil {
			// Return error as tool result
			toolResults = append(toolResults, models.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    fmt.Sprintf("Error: %v", err),
			})
			continue
		}

		// Format the tool result
		resultContent := result.GetTextContent()
		h.logger.Info("MCP tool executed successfully", "tool_name", tc.Function.Name, "result_length", len(resultContent))
		toolResults = append(toolResults, models.Message{
			Role:       "tool",
			ToolCallID: tc.ID,
			Content:    resultContent,
		})
	}

	// If we executed any MCP tools, we need to continue the conversation
	if len(toolResults) > 0 {
		// Build new messages array with original messages + tool results
		// Note: Claude CLI stream-json doesn't accept assistant messages in input,
		// so we include only user messages and tool results
		messages := append([]models.Message{}, req.Messages...)
		messages = append(messages, toolResults...)

		// Create a new request with the tool results
		newReq := &models.ChatCompletionRequest{
			Model:    req.Model,
			Messages: messages,
			Tools:    req.Tools,
		}

		// Execute again to get Claude's response to the tool results
		newCtx, cancel := context.WithTimeout(ctx, getRequestTimeout())
		defer cancel()

		output, err := h.executor.ExecuteWithMessages(newCtx, newReq)
		if err != nil {
			h.logger.Error("failed to execute continuation after tool calls", "error", err.Error())
			return resp
		}

		claudeResp, err := h.parser.ParseJSONResponse(output)
		if err != nil {
			h.logger.Error("failed to parse continuation response", "error", err.Error())
			return resp
		}

		return h.converter.ClaudeToOpenAIResponse(claudeResp, req.Model)
	}

	return resp
}

// handleStreamingCLI handles streaming requests using CLI.
func (h *ChatCompletionsHandler) handleStreamingCLI(c *fiber.Ctx, req *models.ChatCompletionRequest, start time.Time) error {
	// Set SSE headers
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")
	c.Set("X-Accel-Buffering", "no")

	completionID := converter.GenerateCompletionID()

	// When tools are present we cannot stream incrementally: the model emits a
	// tool call as a JSON block inside its text output, and whether the turn is a
	// tool call is only knowable after the whole response is seen. So we buffer
	// the full response, detect tool calls with the same fence-based extraction
	// used in non-streaming, and emulate an SSE stream from the result.
	if len(req.Tools) > 0 {
		return h.handleStreamingCLIWithTools(c, req, start, completionID)
	}

	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		defer func() {
			h.metrics.RecordRequest("success", true, time.Since(start).Seconds())
		}()

		claudeStart := time.Now()

		// Start streaming from Claude CLI (supports images and tools via stream-json)
		chunks, errChan, err := h.executor.ExecuteStreamingWithMessages(context.Background(), req)
		if err != nil {
			h.metrics.RecordError("claude_error")
			h.writeSSEError(w, "Failed to start Claude: "+err.Error())
			return
		}

		h.metrics.RecordClaudeDuration(time.Since(claudeStart).Seconds())

		isFirst := true

		for line := range chunks {
			msg, err := h.parser.ParseStreamLine(line)
			if err != nil {
				continue
			}

			// Handle stream_event messages with content deltas
			if msg.Type == "stream_event" {
				deltaText := msg.GetDeltaText()
				if deltaText == "" {
					continue
				}

				// Send role-only chunk first
				if isFirst {
					roleChunk := h.converter.CreateRoleChunk(completionID, req.Model)
					data, _ := json.Marshal(roleChunk)
					fmt.Fprintf(w, "data: %s\n\n", data)
					w.Flush()
					isFirst = false
				}

				// Create chunk with delta text
				chunk := h.converter.CreateContentChunk(completionID, req.Model, deltaText)
				data, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "data: %s\n\n", data)
				w.Flush()
			}
		}

		// Check for errors
		select {
		case err := <-errChan:
			if err != nil {
				h.metrics.RecordError("claude_error")
				h.writeSSEError(w, err.Error())
				return
			}
		default:
		}

		// Send final chunk with finish_reason
		finalChunk := h.converter.CreateFinalChunk(completionID, req.Model)
		data, _ := json.Marshal(finalChunk)
		fmt.Fprintf(w, "data: %s\n\n", data)

		// Send [DONE] marker
		fmt.Fprintf(w, "data: [DONE]\n\n")
		w.Flush()
	}))

	return nil
}

// handleStreamingCLIWithTools serves a streaming request that carries tool
// definitions. Because tool calls are emulated via the system prompt (the model
// returns a JSON block in its text), real token streaming is impossible: we must
// see the full response before we can tell a tool call from prose. We therefore
// run the non-streaming path — identical extraction and MCP handling as
// handleNonStreamingCLI — then emulate the SSE stream from the final response.
func (h *ChatCompletionsHandler) handleStreamingCLIWithTools(c *fiber.Ctx, req *models.ChatCompletionRequest, start time.Time, completionID string) error {
	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		defer func() {
			h.metrics.RecordRequest("success", true, time.Since(start).Seconds())
		}()

		ctx, cancel := context.WithTimeout(context.Background(), getRequestTimeout())
		defer cancel()

		claudeStart := time.Now()

		// Execute without streaming so we have the complete output to inspect.
		bufReq := *req
		bufReq.Stream = false
		output, err := h.executor.ExecuteWithMessages(ctx, &bufReq)
		if err != nil {
			h.logger.Error("claude execution failed", "error", err.Error(), "model", req.Model)
			h.metrics.RecordError("claude_error")
			h.writeSSEError(w, "Failed to execute Claude: "+err.Error())
			return
		}

		h.metrics.RecordClaudeDuration(time.Since(claudeStart).Seconds())

		claudeResp, err := h.parser.ParseJSONResponse(output)
		if err != nil {
			h.metrics.RecordError("parse_error")
			h.writeSSEError(w, "Failed to parse Claude response: "+err.Error())
			return
		}

		// Same fence-based tool-call extraction as the non-streaming handler.
		openaiResp := h.converter.ClaudeToOpenAIResponse(claudeResp, req.Model)

		// Execute MCP tools server-side and continue, mirroring non-streaming.
		if len(openaiResp.Choices) > 0 && len(openaiResp.Choices[0].Message.ToolCalls) > 0 && h.mcpManager != nil {
			openaiResp = h.executeMCPToolCalls(ctx, openaiResp, req)
		}

		if len(openaiResp.Choices) == 0 {
			h.writeSSEError(w, "Claude returned no choices")
			return
		}

		h.writeEmulatedStream(w, openaiResp.Choices[0], completionID, req.Model)
	}))

	return nil
}

// writeEmulatedStream renders an already-resolved choice as an emulated SSE
// stream: a role-only opening chunk, then either tool-call deltas terminated by
// finish_reason "tool_calls", or text deltas terminated by finish_reason "stop",
// followed by the [DONE] marker.
func (h *ChatCompletionsHandler) writeEmulatedStream(w *bufio.Writer, choice models.Choice, completionID, model string) {
	// Role-only chunk first, matching OpenAI's streaming shape.
	h.writeSSEChunk(w, h.converter.CreateRoleChunk(completionID, model))

	if len(choice.Message.ToolCalls) > 0 {
		// Emit each tool call as: an opening chunk carrying id/type/name, then a
		// chunk carrying the JSON arguments — the canonical OpenAI delta split.
		for i, tc := range choice.Message.ToolCalls {
			h.writeSSEChunk(w, h.converter.CreateToolCallChunk(completionID, model, i, tc.ID, tc.Function.Name, ""))
			if tc.Function.Arguments != "" {
				h.writeSSEChunk(w, h.converter.CreateToolCallChunk(completionID, model, i, "", "", tc.Function.Arguments))
			}
		}
		h.writeSSEChunk(w, h.converter.CreateToolCallFinalChunk(completionID, model))
	} else {
		// Plain text: emulate streaming by splitting the buffered text.
		if text, ok := choice.Message.Content.(string); ok {
			for _, piece := range chunkText(text) {
				h.writeSSEChunk(w, h.converter.CreateContentChunk(completionID, model, piece))
			}
		}
		h.writeSSEChunk(w, h.converter.CreateFinalChunk(completionID, model))
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	w.Flush()
}

// writeSSEChunk marshals a chunk and writes it as a single SSE event.
func (h *ChatCompletionsHandler) writeSSEChunk(w *bufio.Writer, chunk *models.ChatCompletionChunk) {
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	w.Flush()
}

// chunkText splits buffered text into small pieces to emulate token streaming.
// Splitting is rune-based so multi-byte characters (e.g. Persian, emoji) are
// never cut mid-encoding.
func chunkText(text string) []string {
	if text == "" {
		return nil
	}
	const maxChunkRunes = 24
	runes := []rune(text)
	var chunks []string
	for i := 0; i < len(runes); i += maxChunkRunes {
		end := i + maxChunkRunes
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

// writeSSEError writes an error as an SSE event.
func (h *ChatCompletionsHandler) writeSSEError(w *bufio.Writer, message string) {
	errResp := models.ErrorResponse{
		Error: models.ErrorDetail{
			Message: message,
			Type:    "server_error",
			Code:    "claude_error",
		},
	}
	data, _ := json.Marshal(errResp)
	fmt.Fprintf(w, "data: %s\n\n", data)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	w.Flush()
}

// NOTE: Anthropic API handlers removed as part of deprecation (PRP-002).
// All requests now use Claude CLI only.

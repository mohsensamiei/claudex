package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/leeaandrob/claudex/internal/converter"
	"github.com/leeaandrob/claudex/internal/models"
	"github.com/valyala/fasthttp"
)

// MessagesHandler serves the native Anthropic Messages API (POST /v1/messages):
// it accepts requests in Claude format and responds in Claude format. Internally
// it converts to the OpenAI-shaped request, reuses the shared Claude CLI
// pipeline (via the chat handler), and converts the result back to Anthropic
// format.
type MessagesHandler struct {
	chat *ChatCompletionsHandler
}

// NewMessagesHandler creates a new Anthropic messages handler that delegates the
// Claude CLI execution pipeline to the given chat-completions handler.
func NewMessagesHandler(chat *ChatCompletionsHandler) *MessagesHandler {
	return &MessagesHandler{chat: chat}
}

// Handle processes native Anthropic Messages API requests (POST /v1/messages):
// Claude-format request in, Claude-format response out. See docs/openapi.yaml
// for the documented contract.
func (h *MessagesHandler) Handle(c *fiber.Ctx) error {
	start := time.Now()
	h.chat.metrics.IncrementActive()
	defer h.chat.metrics.DecrementActive()

	var req models.AnthropicRequest
	if err := c.BodyParser(&req); err != nil {
		h.chat.metrics.RecordError("parse_error")
		return c.Status(fiber.StatusBadRequest).JSON(
			models.NewAnthropicError("invalid_request_error", "Invalid request body: "+err.Error()),
		)
	}

	if len(req.Messages) == 0 {
		h.chat.metrics.RecordError("validation_error")
		return c.Status(fiber.StatusBadRequest).JSON(
			models.NewAnthropicError("invalid_request_error", "messages: at least one message is required"),
		)
	}

	// Convert the Anthropic request into the internal OpenAI-shaped request so
	// it can flow through the shared Claude CLI pipeline.
	openaiReq, err := h.chat.converter.AnthropicToOpenAIRequest(&req)
	if err != nil {
		h.chat.metrics.RecordError("parse_error")
		return c.Status(fiber.StatusBadRequest).JSON(
			models.NewAnthropicError("invalid_request_error", err.Error()),
		)
	}

	// Add MCP tools, mirroring the OpenAI handler.
	if h.chat.mcpManager != nil && h.chat.mcpManager.HasTools() {
		openaiReq.Tools = append(openaiReq.Tools, h.chat.mcpManager.GetToolsAsOpenAI()...)
	}

	if req.Stream {
		return h.handleStreaming(c, &req, openaiReq, start)
	}
	return h.handleNonStreaming(c, &req, openaiReq, start)
}

// handleNonStreaming runs the buffered pipeline and returns a single Anthropic
// message response.
func (h *MessagesHandler) handleNonStreaming(c *fiber.Ctx, req *models.AnthropicRequest, openaiReq *models.ChatCompletionRequest, start time.Time) error {
	ctx, cancel := context.WithTimeout(c.Context(), getRequestTimeout())
	defer cancel()

	claudeStart := time.Now()
	openaiResp, err := h.chat.generateOpenAIResponse(ctx, openaiReq)
	if err != nil {
		h.chat.logger.Error("claude execution failed", "error", err.Error(), "model", req.Model)
		h.chat.metrics.RecordError("claude_error")
		h.chat.metrics.RecordRequest("error", false, time.Since(start).Seconds())
		return c.Status(fiber.StatusInternalServerError).JSON(
			models.NewAnthropicError("api_error", err.Error()),
		)
	}
	h.chat.metrics.RecordClaudeDuration(time.Since(claudeStart).Seconds())

	anthResp := h.chat.converter.OpenAIToAnthropicResponse(openaiResp, req.Model)
	h.chat.metrics.RecordRequest("success", false, time.Since(start).Seconds())

	return c.JSON(anthResp)
}

// handleStreaming serves a streaming Anthropic response as SSE. When tools are
// present the response is buffered (tool calls can only be detected after the
// full output is seen) and the SSE event sequence is emulated; otherwise text
// is streamed incrementally as content_block_delta events.
func (h *MessagesHandler) handleStreaming(c *fiber.Ctx, req *models.AnthropicRequest, openaiReq *models.ChatCompletionRequest, start time.Time) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")
	c.Set("X-Accel-Buffering", "no")

	if len(openaiReq.Tools) > 0 {
		return h.streamBuffered(c, req, openaiReq, start)
	}
	return h.streamIncremental(c, req, openaiReq, start)
}

// streamIncremental streams plain text responses token-by-token.
func (h *MessagesHandler) streamIncremental(c *fiber.Ctx, req *models.AnthropicRequest, openaiReq *models.ChatCompletionRequest, start time.Time) error {
	conv := h.chat.converter
	model := req.Model

	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		defer func() {
			h.chat.metrics.RecordRequest("success", true, time.Since(start).Seconds())
		}()

		messageID := converter.GenerateMessageID()
		writeAnthropicEvent(w, conv.AnthropicMessageStart(messageID, model, 0))

		claudeStart := time.Now()
		chunks, errChan, err := h.chat.executor.ExecuteStreamingWithMessages(context.Background(), openaiReq)
		if err != nil {
			h.chat.metrics.RecordError("claude_error")
			writeAnthropicSSEError(w, "Failed to start Claude: "+err.Error())
			return
		}
		h.chat.metrics.RecordClaudeDuration(time.Since(claudeStart).Seconds())

		blockOpen := false
		for line := range chunks {
			msg, err := h.chat.parser.ParseStreamLine(line)
			if err != nil {
				continue
			}
			if msg.Type != "stream_event" {
				continue
			}
			deltaText := msg.GetDeltaText()
			if deltaText == "" {
				continue
			}
			if !blockOpen {
				writeAnthropicEvent(w, conv.AnthropicTextBlockStart(0))
				blockOpen = true
			}
			writeAnthropicEvent(w, conv.AnthropicTextDelta(0, deltaText))
		}

		select {
		case err := <-errChan:
			if err != nil {
				h.chat.metrics.RecordError("claude_error")
				if !blockOpen {
					// No content emitted yet; surface the error before closing.
					writeAnthropicSSEError(w, err.Error())
					return
				}
			}
		default:
		}

		if blockOpen {
			writeAnthropicEvent(w, conv.AnthropicBlockStop(0))
		}
		writeAnthropicEvent(w, conv.AnthropicMessageDelta("end_turn", 0))
		writeAnthropicEvent(w, conv.AnthropicMessageStop())
		w.Flush()
	}))

	return nil
}

// streamBuffered runs the non-streaming pipeline (so tool calls can be
// extracted) and then emulates the Anthropic SSE event sequence from the
// resolved response.
func (h *MessagesHandler) streamBuffered(c *fiber.Ctx, req *models.AnthropicRequest, openaiReq *models.ChatCompletionRequest, start time.Time) error {
	conv := h.chat.converter
	model := req.Model

	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		defer func() {
			h.chat.metrics.RecordRequest("success", true, time.Since(start).Seconds())
		}()

		ctx, cancel := context.WithTimeout(context.Background(), getRequestTimeout())
		defer cancel()

		claudeStart := time.Now()
		openaiResp, err := h.chat.generateOpenAIResponse(ctx, openaiReq)
		if err != nil {
			h.chat.logger.Error("claude execution failed", "error", err.Error(), "model", model)
			h.chat.metrics.RecordError("claude_error")
			writeAnthropicSSEError(w, err.Error())
			return
		}
		h.chat.metrics.RecordClaudeDuration(time.Since(claudeStart).Seconds())

		anthResp := conv.OpenAIToAnthropicResponse(openaiResp, model)

		writeAnthropicEvent(w, conv.AnthropicMessageStart(anthResp.ID, model, anthResp.Usage.InputTokens))

		for i, block := range anthResp.Content {
			switch block.Type {
			case "text":
				writeAnthropicEvent(w, conv.AnthropicTextBlockStart(i))
				for _, piece := range chunkText(block.Text) {
					writeAnthropicEvent(w, conv.AnthropicTextDelta(i, piece))
				}
				writeAnthropicEvent(w, conv.AnthropicBlockStop(i))
			case "tool_use":
				writeAnthropicEvent(w, conv.AnthropicToolUseBlockStart(i, block.ID, block.Name))
				input := string(block.Input)
				if input == "" {
					input = "{}"
				}
				writeAnthropicEvent(w, conv.AnthropicInputJSONDelta(i, input))
				writeAnthropicEvent(w, conv.AnthropicBlockStop(i))
			}
		}

		writeAnthropicEvent(w, conv.AnthropicMessageDelta(anthResp.StopReason, anthResp.Usage.OutputTokens))
		writeAnthropicEvent(w, conv.AnthropicMessageStop())
		w.Flush()
	}))

	return nil
}

// writeAnthropicEvent writes a single SSE event in the Anthropic wire format:
// an `event:` line naming the event type followed by its JSON `data:` payload.
func writeAnthropicEvent(w *bufio.Writer, ev converter.SSEEvent) {
	data, _ := json.Marshal(ev.Data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Event, data)
	w.Flush()
}

// writeAnthropicSSEError emits an Anthropic error event and terminates the
// stream.
func writeAnthropicSSEError(w *bufio.Writer, message string) {
	errResp := models.NewAnthropicError("api_error", message)
	data, _ := json.Marshal(errResp)
	fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
	w.Flush()
}

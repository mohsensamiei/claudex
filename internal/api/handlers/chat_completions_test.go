package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/leeaandrob/claudex/internal/converter"
	"github.com/leeaandrob/claudex/internal/models"
)

// newTestHandler builds a handler with only the dependencies writeEmulatedStream
// needs (the converter). The executor/CLI is never touched.
func newTestHandler() *ChatCompletionsHandler {
	return &ChatCompletionsHandler{converter: converter.NewConverter()}
}

// parseSSE captures the chunks the handler wrote and returns the decoded
// chat.completion.chunk events plus whether the [DONE] marker was emitted.
func parseSSE(t *testing.T, raw string) (chunks []models.ChatCompletionChunk, done bool) {
	t.Helper()
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			done = true
			continue
		}
		var chunk models.ChatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("failed to unmarshal SSE chunk %q: %v", payload, err)
		}
		chunks = append(chunks, chunk)
	}
	return chunks, done
}

func runEmulatedStream(t *testing.T, choice models.Choice) ([]models.ChatCompletionChunk, bool) {
	t.Helper()
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	newTestHandler().writeEmulatedStream(w, choice, "chatcmpl-test", "claude-3-5-sonnet")
	w.Flush()
	return parseSSE(t, buf.String())
}

func TestWriteEmulatedStream_ToolCalls(t *testing.T) {
	choice := models.Choice{
		Index: 0,
		Message: models.Message{
			Role:    "assistant",
			Content: nil,
			ToolCalls: []models.ToolCall{
				{
					ID:   "call_Tehran_weather_01",
					Type: "function",
					Function: models.FunctionCall{
						Name:      "get_weather",
						Arguments: `{"city":"Tehran","unit":"celsius"}`,
					},
				},
			},
		},
		FinishReason: "tool_calls",
	}

	chunks, done := runEmulatedStream(t, choice)
	if !done {
		t.Fatal("expected [DONE] marker")
	}
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks (role, tool open, tool args, final), got %d", len(chunks))
	}

	// First chunk: role only, no tool calls or content.
	first := chunks[0].Choices[0].Delta
	if first.Role != "assistant" {
		t.Errorf("first chunk role = %q, want assistant", first.Role)
	}
	if first.Content != "" || len(first.ToolCalls) != 0 {
		t.Errorf("first chunk should carry only the role, got %+v", first)
	}

	// Reassemble tool-call deltas across chunks.
	var gotID, gotType, gotName, gotArgs string
	var sawFinish string
	for _, ch := range chunks[1:] {
		choice := ch.Choices[0]
		if choice.FinishReason != "" {
			sawFinish = choice.FinishReason
		}
		for _, tcd := range choice.Delta.ToolCalls {
			if tcd.Index != 0 {
				t.Errorf("tool call index = %d, want 0", tcd.Index)
			}
			if tcd.ID != "" {
				gotID = tcd.ID
			}
			if tcd.Type != "" {
				gotType = tcd.Type
			}
			if tcd.Function != nil {
				gotName += tcd.Function.Name
				gotArgs += tcd.Function.Arguments
			}
		}
		// No content must leak into the tool-call stream.
		if choice.Delta.Content != "" {
			t.Errorf("tool-call stream leaked content: %q", choice.Delta.Content)
		}
	}

	if gotID != "call_Tehran_weather_01" {
		t.Errorf("tool call id = %q, want call_Tehran_weather_01", gotID)
	}
	if gotType != "function" {
		t.Errorf("tool call type = %q, want function", gotType)
	}
	if gotName != "get_weather" {
		t.Errorf("tool call name = %q, want get_weather", gotName)
	}
	if gotArgs != `{"city":"Tehran","unit":"celsius"}` {
		t.Errorf("tool call arguments = %q, want the celsius Tehran JSON", gotArgs)
	}
	if sawFinish != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", sawFinish)
	}
}

func TestWriteEmulatedStream_PlainText(t *testing.T) {
	// Longer than one chunk so we exercise the splitter.
	text := strings.Repeat("سلام ", 20)
	choice := models.Choice{
		Index: 0,
		Message: models.Message{
			Role:    "assistant",
			Content: text,
		},
		FinishReason: "stop",
	}

	chunks, done := runEmulatedStream(t, choice)
	if !done {
		t.Fatal("expected [DONE] marker")
	}

	if chunks[0].Choices[0].Delta.Role != "assistant" {
		t.Errorf("first chunk should carry the role, got %+v", chunks[0].Choices[0].Delta)
	}

	var rebuilt strings.Builder
	var sawFinish string
	for _, ch := range chunks[1:] {
		choice := ch.Choices[0]
		if len(choice.Delta.ToolCalls) != 0 {
			t.Errorf("plain-text stream leaked tool calls: %+v", choice.Delta.ToolCalls)
		}
		rebuilt.WriteString(choice.Delta.Content)
		if choice.FinishReason != "" {
			sawFinish = choice.FinishReason
		}
	}

	if rebuilt.String() != text {
		t.Errorf("reassembled text mismatch:\n got %q\nwant %q", rebuilt.String(), text)
	}
	if sawFinish != "stop" {
		t.Errorf("finish_reason = %q, want stop", sawFinish)
	}
}

func TestChunkText(t *testing.T) {
	t.Run("empty returns nil", func(t *testing.T) {
		if got := chunkText(""); got != nil {
			t.Errorf("chunkText(\"\") = %v, want nil", got)
		}
	})

	t.Run("short text stays in one chunk", func(t *testing.T) {
		got := chunkText("hello")
		if len(got) != 1 || got[0] != "hello" {
			t.Errorf("chunkText(\"hello\") = %v, want [hello]", got)
		}
	})

	t.Run("reassembly is lossless and multibyte-safe", func(t *testing.T) {
		// Mix of Persian + emoji to ensure no rune is split mid-encoding.
		text := "وضعیت هوای تهران ۲۵ درجه 🌡️ سانتی‌گراد است و آفتابی"
		pieces := chunkText(text)
		if strings.Join(pieces, "") != text {
			t.Errorf("reassembled text != original\n got %q\nwant %q", strings.Join(pieces, ""), text)
		}
		for _, p := range pieces {
			if !json.Valid([]byte(strconvQuote(p))) {
				t.Errorf("chunk is not valid UTF-8 / JSON-encodable: %q", p)
			}
		}
	})
}

// strconvQuote JSON-quotes a string so the test can assert each chunk encodes
// cleanly (no broken multibyte runes).
func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

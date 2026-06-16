package models

// ClaudeJSONResponse represents a non-streaming Claude CLI JSON output.
type ClaudeJSONResponse struct {
	Type       string  `json:"type"`
	Result     string  `json:"result"`
	SessionID  string  `json:"session_id"`
	CostUSD    float64 `json:"cost_usd"`
	DurationMS int     `json:"duration_ms"`
}

// ClaudeStreamMessage represents a streaming Claude CLI output line (NDJSON).
type ClaudeStreamMessage struct {
	Type      string             `json:"type"`
	SessionID string             `json:"session_id,omitempty"`
	Message   *ClaudeMessage     `json:"message,omitempty"`
	Result    string             `json:"result,omitempty"`
	Event     *ClaudeStreamEvent `json:"event,omitempty"` // For stream_event type
}

// ClaudeStreamEvent represents a streaming event from Claude CLI with --include-partial-messages.
type ClaudeStreamEvent struct {
	Type  string            `json:"type"` // message_start, content_block_start, content_block_delta, content_block_stop, message_delta, message_stop
	Index int               `json:"index,omitempty"`
	Delta *ClaudeEventDelta `json:"delta,omitempty"`
}

// ClaudeEventDelta represents the delta in a content_block_delta event.
type ClaudeEventDelta struct {
	Type string `json:"type"` // text_delta
	Text string `json:"text,omitempty"`
}

// ClaudeMessage represents a message in Claude streaming output.
type ClaudeMessage struct {
	Role    string               `json:"role"`
	Content []ClaudeContentBlock `json:"content"`
}

// ClaudeContentBlock represents a content block in Claude message.
type ClaudeContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// GetTextContent extracts all text content from the message.
func (m *ClaudeMessage) GetTextContent() string {
	var result string
	for _, block := range m.Content {
		if block.Type == "text" {
			result += block.Text
		}
	}
	return result
}

// GetDeltaText returns the text delta from a stream_event if available.
func (m *ClaudeStreamMessage) GetDeltaText() string {
	if m.Type != "stream_event" || m.Event == nil {
		return ""
	}
	if m.Event.Type != "content_block_delta" || m.Event.Delta == nil {
		return ""
	}
	if m.Event.Delta.Type != "text_delta" {
		return ""
	}
	return m.Event.Delta.Text
}

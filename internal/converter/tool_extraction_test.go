package converter

import (
	"testing"
)

func TestExtractToolCalls(t *testing.T) {
	conv := NewConverter()

	tests := []struct {
		name           string
		input          string
		wantToolCalls  int
		wantToolName   string
		wantHasContent bool
	}{
		{
			name: "JSON in code fence",
			input: `I'll press the buttons now.

` + "```json\n" + `{
  "tool_calls": [
    {
      "id": "call_123",
      "type": "function",
      "function": {
        "name": "press_buttons",
        "arguments": "{\"buttons\": [\"up\", \"a\"]}"
      }
    }
  ]
}
` + "```",
			wantToolCalls:  1,
			wantToolName:   "press_buttons",
			wantHasContent: true,
		},
		{
			name:           "Raw JSON only",
			input:          `{"tool_calls":[{"id":"call_456","type":"function","function":{"name":"navigate_to","arguments":"{\"row\":5,\"col\":3}"}}]}`,
			wantToolCalls:  1,
			wantToolName:   "navigate_to",
			wantHasContent: false,
		},
		{
			name:           "Arguments as object (not string)",
			input:          `{"tool_calls":[{"id":"call_789","type":"function","function":{"name":"press_buttons","arguments":{"buttons":["down","b"]}}}]}`,
			wantToolCalls:  1,
			wantToolName:   "press_buttons",
			wantHasContent: false,
		},
		{
			name:           "No tool calls - plain text",
			input:          "I see a Pikachu in the grass. Let me think about what to do next.",
			wantToolCalls:  0,
			wantHasContent: true,
		},
		{
			name:           "Multiple tool calls",
			input:          `{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"press_buttons","arguments":"{\"buttons\":[\"a\"]}"}},{"id":"call_2","type":"function","function":{"name":"press_buttons","arguments":"{\"buttons\":[\"b\"]}"}}]}`,
			wantToolCalls:  2,
			wantToolName:   "press_buttons",
			wantHasContent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, toolCalls := conv.ExtractToolCalls(tt.input)

			if len(toolCalls) != tt.wantToolCalls {
				t.Errorf("got %d tool calls, want %d", len(toolCalls), tt.wantToolCalls)
			}

			if tt.wantToolCalls > 0 && toolCalls[0].Function.Name != tt.wantToolName {
				t.Errorf("got tool name %q, want %q", toolCalls[0].Function.Name, tt.wantToolName)
			}

			hasContent := len(content) > 0
			if hasContent != tt.wantHasContent {
				t.Errorf("hasContent=%v, want %v (content=%q)", hasContent, tt.wantHasContent, content)
			}

			// Verify arguments is valid JSON string
			for _, tc := range toolCalls {
				if tc.Function.Arguments == "" {
					t.Errorf("empty arguments for tool %s", tc.Function.Name)
				}
				// Check it starts with { (valid JSON object)
				if tc.Function.Arguments[0] != '{' {
					t.Errorf("arguments not valid JSON: %s", tc.Function.Arguments)
				}
			}
		})
	}
}

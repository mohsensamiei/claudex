package converter

import (
	"testing"

	"github.com/leeaandrob/claudex/internal/models"
)

func TestClaudeUsageToOpenAI(t *testing.T) {
	tests := []struct {
		name             string
		usage            *models.ClaudeUsage
		wantPrompt       int
		wantCompletion   int
		wantTotal        int
		wantDetails      bool
		wantCachedTokens int
	}{
		{
			name:  "nil usage yields zero value",
			usage: nil,
		},
		{
			name: "cache read tokens are reported as cached_tokens",
			usage: &models.ClaudeUsage{
				InputTokens:              200,
				OutputTokens:             96,
				CacheCreationInputTokens: 300,
				CacheReadInputTokens:     16993,
			},
			wantPrompt:       17493,
			wantCompletion:   96,
			wantTotal:        17589,
			wantDetails:      true,
			wantCachedTokens: 16993,
		},
		{
			name: "no cache read omits the breakdown",
			usage: &models.ClaudeUsage{
				InputTokens:  120,
				OutputTokens: 40,
			},
			wantPrompt:     120,
			wantCompletion: 40,
			wantTotal:      160,
			wantDetails:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := claudeUsageToOpenAI(tt.usage)

			if got.PromptTokens != tt.wantPrompt {
				t.Errorf("PromptTokens = %d, want %d", got.PromptTokens, tt.wantPrompt)
			}
			if got.CompletionTokens != tt.wantCompletion {
				t.Errorf("CompletionTokens = %d, want %d", got.CompletionTokens, tt.wantCompletion)
			}
			if got.TotalTokens != tt.wantTotal {
				t.Errorf("TotalTokens = %d, want %d", got.TotalTokens, tt.wantTotal)
			}

			if tt.wantDetails {
				if got.PromptTokensDetails == nil {
					t.Fatalf("PromptTokensDetails = nil, want cached_tokens %d", tt.wantCachedTokens)
				}
				if got.PromptTokensDetails.CachedTokens != tt.wantCachedTokens {
					t.Errorf("CachedTokens = %d, want %d", got.PromptTokensDetails.CachedTokens, tt.wantCachedTokens)
				}
			} else if got.PromptTokensDetails != nil {
				t.Errorf("PromptTokensDetails = %+v, want nil", got.PromptTokensDetails)
			}
		})
	}
}

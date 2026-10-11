package oaichat

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIChatRequestToClaudeMessagesReasoningDisabled(t *testing.T) {
	for _, modelName := range []string{"claude-sonnet-4-thinking", "claude-opus-4-6-high"} {
		t.Run(modelName, func(t *testing.T) {
			maxTokens := uint(256)
			request := dto.GeneralOpenAIRequest{
				Model: modelName, MaxTokens: &maxTokens,
				Messages:  []dto.Message{{Role: "user", Content: "1+1?"}},
				Reasoning: []byte(`{"enabled":false,"max_tokens":2048}`),
			}
			meta := &convmeta.Values{Options: &convmeta.Options{Claude: convmeta.ClaudeOptions{
				ThinkingAdapterEnabled: true, ThinkingAdapterBudgetTokensPercentage: 0.8,
			}}}

			converted, err := OpenAIChatRequestToClaudeMessages(context.Background(), meta, request)

			require.NoError(t, err)
			require.NotNil(t, converted.Thinking)
			assert.Equal(t, "disabled", converted.Thinking.Type)
			assert.Nil(t, converted.Thinking.BudgetTokens)
			assert.Empty(t, converted.OutputConfig)
			assert.Equal(t, &maxTokens, converted.MaxTokens)
			assert.Nil(t, converted.Temperature)
		})
	}
}

func TestOpenAIChatRequestToClaudeMessagesReasoningBudgetWithoutEnabled(t *testing.T) {
	maxTokens := uint(2304)
	converted, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
		Model: "claude-sonnet-4", MaxTokens: &maxTokens,
		Messages:  []dto.Message{{Role: "user", Content: "1+1?"}},
		Reasoning: []byte(`{"max_tokens":2048}`),
	})
	require.NoError(t, err)
	require.NotNil(t, converted.Thinking)
	assert.Equal(t, "enabled", converted.Thinking.Type)
	require.NotNil(t, converted.Thinking.BudgetTokens)
	assert.Equal(t, 2048, *converted.Thinking.BudgetTokens)
}

func TestOpenAIChatRequestToClaudeMessagesAdaptiveReasoningEffort(t *testing.T) {
	for _, tc := range []struct {
		model, reasoning, wantType, wantEffort string
		wantBudget                             *int
	}{
		{"claude-opus-4-7-high", `{"effort":"low"}`, "adaptive", "low", nil},
		{"anthropic.claude-opus-4-7", `{"effort":"medium"}`, "adaptive", "medium", nil},
		{"us.anthropic.claude-opus-4-8", `{"effort":"high"}`, "adaptive", "high", nil},
		{"claude-opus-4-6-high", `{"effort":"low"}`, "adaptive", "low", nil},
		{"claude-opus-4-7-high", `{"enabled":false,"effort":"low"}`, "disabled", "", nil},
		{"claude-sonnet-4", `{"effort":"low"}`, "", "", nil},
		{"claude-sonnet-4", `{"effort":"low","max_tokens":2048}`, "enabled", "", ptr(2048)},
	} {
		t.Run(tc.model+"/"+tc.reasoning, func(t *testing.T) {
			maxTokens := uint(4352)
			converted, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
				Model: tc.model, MaxTokens: &maxTokens, Reasoning: []byte(tc.reasoning),
				Messages: []dto.Message{{Role: "user", Content: "1+1?"}},
			})
			require.NoError(t, err)
			if tc.wantType == "" {
				assert.Nil(t, converted.Thinking)
			} else {
				require.NotNil(t, converted.Thinking)
				assert.Equal(t, tc.wantType, converted.Thinking.Type)
				assert.Equal(t, tc.wantBudget, converted.Thinking.BudgetTokens)
			}
			assert.Equal(t, tc.wantEffort, gjson.GetBytes(converted.OutputConfig, "effort").String())
			if tc.wantType == "adaptive" && tc.model != "claude-opus-4-6-high" {
				assert.Equal(t, "summarized", converted.Thinking.Display)
				assert.Nil(t, converted.Temperature)
				assert.Nil(t, converted.TopP)
				assert.Nil(t, converted.TopK)
			}
		})
	}
}

func TestOpenAIChatRequestToClaudeMessagesReasoningDisabledPreservesSamplingRestrictions(t *testing.T) {
	maxTokens, temperature, topP, topK := uint(256), 0.0, 0.5, 20
	converted, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
		Model: "claude-opus-4-7-high", MaxTokens: &maxTokens,
		Temperature: &temperature, TopP: &topP, TopK: &topK,
		Messages:  []dto.Message{{Role: "user", Content: "1+1?"}},
		Reasoning: []byte(`{"enabled":false}`),
	})
	require.NoError(t, err)
	require.NotNil(t, converted.Thinking)
	assert.Equal(t, "disabled", converted.Thinking.Type)
	assert.Nil(t, converted.Temperature)
	assert.Nil(t, converted.TopP)
	assert.Nil(t, converted.TopK)
}

func TestOpenAIChatRequestToClaudeMessagesNormalizesToolInputSchema(t *testing.T) {
	tests := []struct {
		name       string
		parameters any
		wantSchema map[string]any
	}{
		{
			name:       "omitted parameters",
			parameters: nil,
			wantSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			name: "missing type and properties",
			parameters: map[string]any{
				"additionalProperties": false,
			},
			wantSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			name: "non-string type",
			parameters: map[string]any{
				"type":       123,
				"properties": map[string]any{},
			},
			wantSchema: map[string]any{
				"type":       123,
				"properties": map[string]any{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxTokens := uint(1024)
			got, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
				Model:     "claude-test",
				MaxTokens: &maxTokens,
				Messages: []dto.Message{
					{Role: "user", Content: "Call the tool."},
				},
				Tools: []dto.ToolCallRequest{
					{
						Type: "function",
						Function: dto.FunctionRequest{
							Name:        "get_current_time",
							Description: "Get the current time",
							Parameters:  tt.parameters,
						},
					},
				},
			})

			require.NoError(t, err)
			tools, ok := got.Tools.([]any)
			require.True(t, ok)
			require.Len(t, tools, 1)
			tool, ok := tools[0].(*dto.Tool)
			require.True(t, ok)
			assert.Equal(t, "get_current_time", tool.Name)
			assert.Equal(t, tt.wantSchema, tool.InputSchema)
		})
	}
}

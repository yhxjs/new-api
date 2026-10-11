package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func uintPtr(v uint) *uint { return &v }

func TestConvertOpenAIRequestMaxTokensExclusive(t *testing.T) {
	mkInfo := func(upstreamModel string, channelType int, baseURL string) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			ChannelMeta: &relaycommon.ChannelMeta{
				UpstreamModelName: upstreamModel,
				ChannelType:       channelType,
				ChannelBaseUrl:    baseURL,
			},
		}
	}

	tests := []struct {
		name            string
		upstreamModel   string
		channelType     int
		baseURL         string
		maxTokens       *uint
		maxCompTokens   *uint
		wantMaxTokens   *uint
		wantMaxCompDone *uint
	}{
		{
			// 官方 OpenAI 拒绝同时下发两个上限，保留 max_completion_tokens
			name:            "both set keeps max_completion_tokens on official OpenAI",
			upstreamModel:   "gpt-4o",
			channelType:     constant.ChannelTypeOpenAI,
			maxTokens:       uintPtr(256),
			maxCompTokens:   uintPtr(1280),
			wantMaxTokens:   nil,
			wantMaxCompDone: uintPtr(1280),
		},
		{
			name:            "both set on o-model keeps max_completion_tokens on official OpenAI",
			upstreamModel:   "o3-mini",
			channelType:     constant.ChannelTypeOpenAI,
			maxTokens:       uintPtr(256),
			maxCompTokens:   uintPtr(1280),
			wantMaxTokens:   nil,
			wantMaxCompDone: uintPtr(1280),
		},
		{
			name:            "both set on Azure keeps max_completion_tokens",
			upstreamModel:   "gpt-4o",
			channelType:     constant.ChannelTypeAzure,
			maxTokens:       uintPtr(256),
			maxCompTokens:   uintPtr(1280),
			wantMaxTokens:   nil,
			wantMaxCompDone: uintPtr(1280),
		},
		{
			// 兼容渠道的上游可能只识别旧参数，两参数原样下发避免丢失输出上限
			name:            "both set on compatible channel keeps both",
			upstreamModel:   "gpt-4o",
			channelType:     constant.ChannelTypeDeepSeek,
			maxTokens:       uintPtr(256),
			maxCompTokens:   uintPtr(1280),
			wantMaxTokens:   uintPtr(256),
			wantMaxCompDone: uintPtr(1280),
		},
		{
			// o 系列只带 max_tokens 时仍转换为 max_completion_tokens
			name:            "o-model converts max_tokens",
			upstreamModel:   "o3-mini",
			channelType:     constant.ChannelTypeOpenAI,
			maxTokens:       uintPtr(256),
			wantMaxTokens:   nil,
			wantMaxCompDone: uintPtr(256),
		},
		{
			name:            "non-reasoning model keeps max_tokens only",
			upstreamModel:   "gpt-4o",
			channelType:     constant.ChannelTypeOpenAI,
			maxTokens:       uintPtr(256),
			wantMaxTokens:   uintPtr(256),
			wantMaxCompDone: nil,
		},
		{
			// 显式 0 仍是已设置的字段，不能被旧字段覆盖。
			name:            "zero max_completion_tokens remains explicit on o-model",
			upstreamModel:   "o3-mini",
			channelType:     constant.ChannelTypeOpenAI,
			maxTokens:       uintPtr(256),
			maxCompTokens:   uintPtr(0),
			wantMaxTokens:   nil,
			wantMaxCompDone: uintPtr(0),
		},
		{
			name:            "custom OpenAI upstream keeps the legacy limit",
			upstreamModel:   "custom-chat",
			channelType:     constant.ChannelTypeOpenAI,
			baseURL:         "https://compatible.example",
			maxTokens:       uintPtr(256),
			maxCompTokens:   uintPtr(1280),
			wantMaxTokens:   uintPtr(256),
			wantMaxCompDone: uintPtr(1280),
		},
		{
			name:            "custom OpenAI upstream keeps both limits for known reasoning model",
			upstreamModel:   "o3-mini",
			channelType:     constant.ChannelTypeOpenAI,
			baseURL:         "https://compatible.example",
			maxTokens:       uintPtr(256),
			maxCompTokens:   uintPtr(1280),
			wantMaxTokens:   uintPtr(256),
			wantMaxCompDone: uintPtr(1280),
		},
		{
			name:            "custom OpenAI upstream preserves explicit zero completion limit",
			upstreamModel:   "o3-mini",
			channelType:     constant.ChannelTypeOpenAI,
			baseURL:         "https://compatible.example",
			maxTokens:       uintPtr(256),
			maxCompTokens:   uintPtr(0),
			wantMaxTokens:   uintPtr(256),
			wantMaxCompDone: uintPtr(0),
		},
		{
			name:            "official OpenAI URL keeps only the completion limit",
			upstreamModel:   "gpt-4o",
			channelType:     constant.ChannelTypeOpenAI,
			baseURL:         "https://API.OPENAI.COM/",
			maxTokens:       uintPtr(256),
			maxCompTokens:   uintPtr(1280),
			wantMaxCompDone: uintPtr(1280),
		},
		{
			name:            "OpenAI hostname prefix on custom upstream keeps both limits",
			upstreamModel:   "gpt-4o",
			channelType:     constant.ChannelTypeOpenAI,
			baseURL:         "https://api.openai.com.compatible.example",
			maxTokens:       uintPtr(256),
			maxCompTokens:   uintPtr(1280),
			wantMaxTokens:   uintPtr(256),
			wantMaxCompDone: uintPtr(1280),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &dto.GeneralOpenAIRequest{
				Model:               tt.upstreamModel,
				MaxTokens:           tt.maxTokens,
				MaxCompletionTokens: tt.maxCompTokens,
			}

			convertedValue, err := (&Adaptor{}).ConvertOpenAIRequest(nil, mkInfo(tt.upstreamModel, tt.channelType, tt.baseURL), request)
			require.NoError(t, err)
			converted, ok := convertedValue.(*dto.GeneralOpenAIRequest)
			require.True(t, ok)

			if tt.wantMaxTokens == nil {
				assert.Nil(t, converted.MaxTokens)
			} else {
				require.NotNil(t, converted.MaxTokens)
				assert.Equal(t, *tt.wantMaxTokens, *converted.MaxTokens)
			}

			if tt.wantMaxCompDone == nil {
				assert.Nil(t, converted.MaxCompletionTokens)
			} else {
				require.NotNil(t, converted.MaxCompletionTokens)
				assert.Equal(t, *tt.wantMaxCompDone, *converted.MaxCompletionTokens)
			}
		})
	}
}

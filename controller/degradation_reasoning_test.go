package controller

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/deepseek"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/ollama"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/degradation_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestDegradationDeepSeekReasoningRequest(t *testing.T) {
	for _, tc := range []struct {
		model, effort, wantEffort string
		wantLimit                 int64
	}{
		{"deepseek-v4-flash", "low", "low", 1280},
		{"deepseek-v4-flash", "medium", "medium", 2304},
		{"deepseek-v4-flash", "high", "high", 4352},
		{"deepseek-v4-flash-none", "high", "high", 4352},
		{"deepseek-v4-flash-max", "high", "high", 4352},
		{"deepseek-v4-flash", "off", "none", 256},
		{"deepseek-v4-flash-none", "off", "none", 256},
		{"deepseek-v4-flash-max", "off", "none", 256},
	} {
		t.Run(tc.model+"/"+tc.effort, func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{Model: tc.model, MaxTokens: lo.ToPtr(uint(256)), Temperature: lo.ToPtr(0.0)}
			require.NoError(t, applyProbeReasoningParams(req, &degradation_setting.ModelProbeBinding{ReasoningEffort: tc.effort}, 256, constant.APITypeDeepSeek))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: req.Model}, OriginModelName: tc.model}
			value, err := (&deepseek.Adaptor{}).ConvertOpenAIRequest(c, info, req)
			require.NoError(t, err)
			body, err := common.Marshal(value)
			require.NoError(t, err)
			assert.Equal(t, "deepseek-v4-flash", gjson.GetBytes(body, "model").String())
			assert.Equal(t, tc.wantEffort, gjson.GetBytes(body, "reasoning_effort").String())
			assert.Equal(t, tc.wantLimit, gjson.GetBytes(body, "max_tokens").Int())
			assert.False(t, gjson.GetBytes(body, "max_completion_tokens").Exists())
			assert.False(t, gjson.GetBytes(body, "thinking").Exists())
			assert.False(t, gjson.GetBytes(body, "temperature").Exists())
			assert.Equal(t, "deepseek-v4-flash", info.UpstreamModelName)
			assert.Equal(t, tc.model, info.OriginModelName)
		})
	}
}

func TestDegradationDeepSeekInheritedThinkingSuffix(t *testing.T) {
	for _, tc := range []struct {
		model, wantThinking, wantEffort string
	}{
		{"deepseek-v4-flash-none", "disabled", ""},
		{"deepseek-v4-flash-max", "enabled", "max"},
	} {
		for _, binding := range []*degradation_setting.ModelProbeBinding{nil, {}} {
			t.Run(fmt.Sprintf("%s/nil-binding=%t", tc.model, binding == nil), func(t *testing.T) {
				req := &dto.GeneralOpenAIRequest{Model: tc.model, MaxTokens: lo.ToPtr(uint(256))}
				require.NoError(t, applyProbeReasoningParams(req, binding, 256, constant.APITypeDeepSeek))
				assert.Equal(t, tc.model, req.Model)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: req.Model}, OriginModelName: tc.model}
				value, err := (&deepseek.Adaptor{}).ConvertOpenAIRequest(c, info, req)
				require.NoError(t, err)
				body, err := common.Marshal(value)
				require.NoError(t, err)
				assert.Equal(t, "deepseek-v4-flash", gjson.GetBytes(body, "model").String())
				assert.Equal(t, tc.wantThinking, gjson.GetBytes(body, "thinking.type").String())
				assert.Equal(t, tc.wantEffort, gjson.GetBytes(body, "reasoning_effort").String())
				assert.Equal(t, int64(256), gjson.GetBytes(body, "max_tokens").Int())
				assert.False(t, gjson.GetBytes(body, "max_completion_tokens").Exists())
				assert.Equal(t, tc.model, info.OriginModelName)
			})
		}
	}
}

func TestDegradationOllamaReasoningOffRequest(t *testing.T) {
	req := &dto.GeneralOpenAIRequest{Model: "gpt-oss:20b", MaxTokens: lo.ToPtr(uint(256)), Temperature: lo.ToPtr(0.0)}
	require.NoError(t, applyProbeReasoningParams(req, &degradation_setting.ModelProbeBinding{ReasoningEffort: "off"}, 256, constant.APITypeOllama))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: req.Model}, OriginModelName: req.Model}
	value, err := (&ollama.Adaptor{}).ConvertOpenAIRequest(c, info, req)
	require.NoError(t, err)
	body, err := common.Marshal(value)
	require.NoError(t, err)
	assert.Equal(t, "gpt-oss:20b", gjson.GetBytes(body, "model").String())
	assert.Equal(t, "false", gjson.GetBytes(body, "think").Raw)
	assert.Equal(t, int64(256), gjson.GetBytes(body, "options.num_predict").Int())
	assert.False(t, gjson.GetBytes(body, "options.temperature").Exists())
}

func TestDegradationNativeProvidersRejectCustomThinkingBudget(t *testing.T) {
	for _, tc := range []struct {
		model   string
		apiType int
	}{
		{"deepseek-v4-flash", constant.APITypeDeepSeek},
		{"gpt-oss:20b", constant.APITypeOllama},
	} {
		t.Run(tc.model, func(t *testing.T) {
			err := applyProbeReasoningParams(&dto.GeneralOpenAIRequest{Model: tc.model}, &degradation_setting.ModelProbeBinding{ReasoningEffort: "custom", ThinkingBudget: 2048}, 256, tc.apiType)
			require.ErrorContains(t, err, "custom thinking budget is not supported by this provider")
		})
	}
}

func TestDegradationUnknownProviderRejectsReasoningOff(t *testing.T) {
	err := applyProbeReasoningParams(&dto.GeneralOpenAIRequest{Model: "unknown"}, &degradation_setting.ModelProbeBinding{ReasoningEffort: "off"}, 256, -1)
	require.ErrorContains(t, err, "reasoning off is not supported by this provider")
}

func TestDegradationClaudeReasoningRequest(t *testing.T) {
	for _, tc := range []struct {
		model, effort string
		budget        int
	}{
		{"claude-sonnet-4", "low", 1280},
		{"claude-sonnet-4", "medium", 2048},
		{"claude-sonnet-4", "high", 4096},
		{"claude-sonnet-4", "custom", 2048},
		{"claude-opus-4-6-high", "custom", 2048},
	} {
		t.Run(tc.model+"/"+tc.effort, func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{
				Model: tc.model, MaxTokens: lo.ToPtr(uint(256)), Temperature: lo.ToPtr(0.0),
				Messages: []dto.Message{{Role: "user", Content: "123 + 456?"}},
			}
			binding := &degradation_setting.ModelProbeBinding{ReasoningEffort: tc.effort, ThinkingBudget: tc.budget}
			require.NoError(t, applyProbeReasoningParams(req, binding, 256, constant.APITypeAnthropic))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: req.Model}, OriginModelName: req.Model}
			value, err := (&claude.Adaptor{}).ConvertOpenAIRequest(c, info, req)
			require.NoError(t, err)
			converted := value.(*dto.ClaudeRequest)
			require.NotNil(t, converted.Thinking)
			assert.Equal(t, "enabled", converted.Thinking.Type)
			assert.Empty(t, converted.OutputConfig)
			require.NotNil(t, converted.Thinking.BudgetTokens)
			assert.Equal(t, tc.budget, *converted.Thinking.BudgetTokens)
			require.NotNil(t, converted.MaxTokens)
			assert.Equal(t, uint(tc.budget+256), *converted.MaxTokens)
			if converted.Temperature != nil {
				assert.Equal(t, 1.0, *converted.Temperature)
			}
		})
	}
}

func TestDegradationClaudeAdaptiveReasoningRequest(t *testing.T) {
	for _, tc := range []struct {
		model, effort, wantModel string
		apiType                  int
		wantLimit                int64
	}{
		{"claude-opus-4-7", "low", "claude-opus-4-7", constant.APITypeAnthropic, 1280},
		{"claude-opus-4-7", "medium", "claude-opus-4-7", constant.APITypeAnthropic, 2304},
		{"claude-opus-4-7", "high", "claude-opus-4-7", constant.APITypeAnthropic, 4352},
		{"claude-opus-4-7-high", "low", "claude-opus-4-7", constant.APITypeAnthropic, 1280},
		{"claude-opus-4-7-thinking", "medium", "claude-opus-4-7", constant.APITypeVertexAi, 2304},
		{"claude-opus-4-8-max", "high", "claude-opus-4-8", constant.APITypeVertexAi, 4352},
		{"anthropic.claude-opus-4-7-high", "low", "anthropic.claude-opus-4-7", constant.APITypeAws, 1280},
		{"us.anthropic.claude-opus-4-8-thinking", "medium", "us.anthropic.claude-opus-4-8", constant.APITypeAws, 2304},
		{"claude-opus-4-6-high", "low", "claude-opus-4-6", constant.APITypeAnthropic, 1280},
	} {
		t.Run(fmt.Sprintf("%s/%s/%d", tc.model, tc.effort, tc.apiType), func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{
				Model: tc.model, MaxTokens: lo.ToPtr(uint(256)),
				Temperature: lo.ToPtr(0.0), TopP: lo.ToPtr(0.5), TopK: lo.ToPtr(20),
				Messages: []dto.Message{{Role: "user", Content: "1+1?"}},
			}
			require.NoError(t, applyProbeReasoningParams(req, &degradation_setting.ModelProbeBinding{ReasoningEffort: tc.effort}, 256, tc.apiType))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: req.Model}, OriginModelName: tc.model}
			adaptor := relay.GetAdaptor(tc.apiType)
			adaptor.Init(info)
			value, err := adaptor.ConvertOpenAIRequest(c, info, req)
			require.NoError(t, err)
			body, err := common.Marshal(value)
			require.NoError(t, err)
			assert.Equal(t, "adaptive", gjson.GetBytes(body, "thinking.type").String())
			assert.Equal(t, tc.effort, gjson.GetBytes(body, "output_config.effort").String())
			assert.False(t, gjson.GetBytes(body, "thinking.budget_tokens").Exists())
			assert.Equal(t, tc.wantLimit, gjson.GetBytes(body, "max_tokens").Int())
			if tc.wantModel != "claude-opus-4-6" {
				assert.Equal(t, "summarized", gjson.GetBytes(body, "thinking.display").String())
				for _, field := range []string{"temperature", "top_p", "top_k"} {
					assert.False(t, gjson.GetBytes(body, field).Exists(), field)
				}
			}
			assert.Equal(t, tc.wantModel, req.Model)
			if tc.apiType == constant.APITypeVertexAi {
				assert.Equal(t, tc.wantModel, info.UpstreamModelName)
			} else {
				assert.Equal(t, tc.wantModel, gjson.GetBytes(body, "model").String())
			}
			assert.Equal(t, tc.model, info.OriginModelName)
		})
	}
}

func TestDegradationClaudeAdaptiveModelRejectsCustomBudget(t *testing.T) {
	for _, tc := range []struct {
		model   string
		apiType int
	}{
		{"claude-opus-4-7", constant.APITypeAnthropic},
		{"claude-opus-4-8-thinking", constant.APITypeVertexAi},
		{"anthropic.claude-opus-4-7", constant.APITypeAws},
		{"anthropic/claude-opus-4.7", constant.APITypeOpenRouter},
	} {
		t.Run(tc.model, func(t *testing.T) {
			err := applyProbeReasoningParams(&dto.GeneralOpenAIRequest{Model: tc.model}, &degradation_setting.ModelProbeBinding{ReasoningEffort: "custom", ThinkingBudget: 2048}, 256, tc.apiType)
			require.ErrorContains(t, err, "supports adaptive thinking instead of custom token budgets")
		})
	}
}

func TestDegradationOpenRouterClaudeReasoningEffortRequest(t *testing.T) {
	for _, modelName := range []string{"anthropic/claude-sonnet-4", "anthropic/claude-opus-4.7"} {
		for _, tc := range []struct {
			effort    string
			wantLimit int64
		}{{"low", 1280}, {"medium", 2304}, {"high", 4352}} {
			t.Run(modelName+"/"+tc.effort, func(t *testing.T) {
				req := &dto.GeneralOpenAIRequest{Model: modelName, MaxTokens: lo.ToPtr(uint(256)), Temperature: lo.ToPtr(0.0)}
				require.NoError(t, applyProbeReasoningParams(req, &degradation_setting.ModelProbeBinding{ReasoningEffort: tc.effort}, 256, constant.APITypeOpenRouter))
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, UpstreamModelName: req.Model}, OriginModelName: modelName}
				value, err := (&openai.Adaptor{}).ConvertOpenAIRequest(c, info, req)
				require.NoError(t, err)
				body, err := common.Marshal(value)
				require.NoError(t, err)
				assert.Equal(t, tc.effort, gjson.GetBytes(body, "reasoning.effort").String())
				assert.True(t, gjson.GetBytes(body, "reasoning.enabled").Bool())
				assert.False(t, gjson.GetBytes(body, "reasoning.max_tokens").Exists())
				assert.False(t, gjson.GetBytes(body, "reasoning_effort").Exists())
				assert.False(t, gjson.GetBytes(body, "temperature").Exists())
				assert.Equal(t, tc.wantLimit, gjson.GetBytes(body, "max_tokens").Int())
				assert.Equal(t, modelName, gjson.GetBytes(body, "model").String())
			})
		}
	}
}

func TestDegradationClaudeAdaptiveModelOmitsSamplingWithoutReasoningBinding(t *testing.T) {
	for _, binding := range []*degradation_setting.ModelProbeBinding{nil, {}} {
		req := &dto.GeneralOpenAIRequest{
			Model: "claude-opus-4-7", MaxTokens: lo.ToPtr(uint(256)),
			Temperature: lo.ToPtr(0.0), TopP: lo.ToPtr(0.5), TopK: lo.ToPtr(20),
			Messages: []dto.Message{{Role: "user", Content: "1+1?"}},
		}
		require.NoError(t, applyProbeReasoningParams(req, binding, 256, constant.APITypeAnthropic))
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: req.Model}, OriginModelName: req.Model}
		value, err := (&claude.Adaptor{}).ConvertOpenAIRequest(c, info, req)
		require.NoError(t, err)
		body, err := common.Marshal(value)
		require.NoError(t, err)
		for _, field := range []string{"temperature", "top_p", "top_k", "thinking", "output_config"} {
			assert.False(t, gjson.GetBytes(body, field).Exists(), field)
		}
		assert.Equal(t, int64(256), gjson.GetBytes(body, "max_tokens").Int())
		assert.Equal(t, "claude-opus-4-7", gjson.GetBytes(body, "model").String())
	}
}

func TestDegradationGeminiReasoningRequest(t *testing.T) {
	for _, tc := range []struct {
		model, effort string
		budget        int
		level         string
	}{
		{"gemini-2.5-flash", "low", 1024, ""},
		{"gemini-2.5-flash", "custom", 2048, ""},
		{"gemini-3-flash-preview", "high", 4096, "high"},
		{"gemini-3.1-pro-preview", "medium", 2048, "medium"},
	} {
		t.Run(tc.model+"/"+tc.effort, func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{Model: tc.model, MaxTokens: lo.ToPtr(uint(256)), Messages: []dto.Message{{Role: "user", Content: "123 + 456?"}}}
			require.NoError(t, applyProbeReasoningParams(req, &degradation_setting.ModelProbeBinding{ReasoningEffort: tc.effort, ThinkingBudget: tc.budget}, 256, constant.APITypeGemini))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: req.Model}, OriginModelName: req.Model}
			value, err := (&gemini.Adaptor{}).ConvertOpenAIRequest(c, info, req)
			require.NoError(t, err)
			converted := value.(*dto.GeminiChatRequest)
			require.NotNil(t, converted.GenerationConfig.ThinkingConfig)
			if tc.level != "" {
				assert.Equal(t, tc.level, converted.GenerationConfig.ThinkingConfig.ThinkingLevel)
				assert.Nil(t, converted.GenerationConfig.ThinkingConfig.ThinkingBudget)
			} else {
				assert.Equal(t, lo.ToPtr(tc.budget), converted.GenerationConfig.ThinkingConfig.ThinkingBudget)
			}
			assert.Equal(t, lo.ToPtr(uint(tc.budget+256)), converted.GenerationConfig.MaxOutputTokens)
		})
	}
}

func TestDegradationOpenRouterCustomReasoningRequest(t *testing.T) {
	req := &dto.GeneralOpenAIRequest{Model: "anthropic/claude-sonnet-4", MaxTokens: lo.ToPtr(uint(256)), Temperature: lo.ToPtr(0.0)}
	require.NoError(t, applyProbeReasoningParams(req, &degradation_setting.ModelProbeBinding{ReasoningEffort: "custom", ThinkingBudget: 2048}, 256, constant.APITypeOpenRouter))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, UpstreamModelName: req.Model}, OriginModelName: req.Model}
	value, err := (&openai.Adaptor{}).ConvertOpenAIRequest(c, info, req)
	require.NoError(t, err)
	body, err := common.Marshal(value)
	require.NoError(t, err)
	assert.Equal(t, int64(2048), gjson.GetBytes(body, "reasoning.max_tokens").Int())
	assert.Equal(t, int64(2304), gjson.GetBytes(body, "max_tokens").Int())
	assert.False(t, gjson.GetBytes(body, "reasoning_effort").Exists())
}

func TestDegradationGeminiExplicitReasoningOverridesNoThinkingSuffix(t *testing.T) {
	settings := model_setting.GetGeminiSettings()
	oldEnabled := settings.ThinkingAdapterEnabled
	t.Cleanup(func() { settings.ThinkingAdapterEnabled = oldEnabled })
	for _, apiType := range []int{constant.APITypeGemini, constant.APITypeVertexAi} {
		for _, adapterEnabled := range []bool{false, true} {
			for _, tc := range []struct {
				effort string
				budget int
			}{
				{"high", 4096}, {"custom", 2048}, {"", 0},
			} {
				t.Run(fmt.Sprintf("%d/adapter=%t/effort=%s", apiType, adapterEnabled, tc.effort), func(t *testing.T) {
					settings.ThinkingAdapterEnabled = adapterEnabled
					const modelName = "gemini-2.5-flash-nothinking"
					req := &dto.GeneralOpenAIRequest{
						Model: modelName, MaxTokens: lo.ToPtr(uint(256)),
						Messages: []dto.Message{{Role: "user", Content: "1+1?"}},
					}
					require.NoError(t, applyProbeReasoningParams(req, &degradation_setting.ModelProbeBinding{ReasoningEffort: tc.effort, ThinkingBudget: tc.budget}, 256, apiType))
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: req.Model}, OriginModelName: modelName}
					adaptor := relay.GetAdaptor(apiType)
					adaptor.Init(info)
					value, err := adaptor.ConvertOpenAIRequest(c, info, req)
					require.NoError(t, err)
					converted := value.(*dto.GeminiChatRequest)
					thinking := converted.GenerationConfig.ThinkingConfig
					if tc.effort == "" {
						assert.Equal(t, modelName, req.Model)
						if adapterEnabled {
							require.NotNil(t, thinking)
							assert.Equal(t, lo.ToPtr(0), thinking.ThinkingBudget)
						} else {
							assert.Nil(t, thinking)
						}
					} else {
						assert.Equal(t, "gemini-2.5-flash", info.UpstreamModelName)
						require.NotNil(t, thinking)
						assert.Equal(t, lo.ToPtr(tc.budget), thinking.ThinkingBudget)
						assert.True(t, thinking.IncludeThoughts)
					}
					assert.Equal(t, lo.ToPtr(uint(tc.budget+256)), converted.GenerationConfig.MaxOutputTokens)
				})
			}
		}
	}
}

func TestDegradationRejectsUnsupportedThinkingBudget(t *testing.T) {
	for _, tc := range []struct {
		apiType int
		model   string
		budget  int
	}{
		{constant.APITypeOpenAI, "gpt-5", 2048},
		{constant.APITypeAnthropic, "claude-sonnet-4", 1023},
		{constant.APITypeGemini, "gemini-2.5-flash", 24577},
		{constant.APITypeGemini, "gemini-2.5-flash-lite", 511},
		{constant.APITypeGemini, "gemini-3-pro-preview", 2048},
	} {
		t.Run(tc.model, func(t *testing.T) {
			err := applyProbeReasoningParams(&dto.GeneralOpenAIRequest{Model: tc.model}, &degradation_setting.ModelProbeBinding{ReasoningEffort: "custom", ThinkingBudget: tc.budget}, 256, tc.apiType)
			require.Error(t, err)
		})
	}
}

func TestDegradationGeminiReasoningOffRequest(t *testing.T) {
	for _, tc := range []struct {
		model   string
		apiType int
	}{
		{"gemini-2.5-flash", constant.APITypeGemini},
		{"gemini-2.5-flash-lite", constant.APITypeGemini},
		{"gemini-2.5-flash-nothinking", constant.APITypeGemini},
		{"gemini-2.5-flash", constant.APITypeVertexAi},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.model, tc.apiType), func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{Model: tc.model, MaxTokens: lo.ToPtr(uint(256)), Messages: []dto.Message{{Role: "user", Content: "1+1?"}}}
			require.NoError(t, applyProbeReasoningParams(req, &degradation_setting.ModelProbeBinding{ReasoningEffort: "off"}, 256, tc.apiType))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: req.Model}, OriginModelName: req.Model}
			adaptor := relay.GetAdaptor(tc.apiType)
			adaptor.Init(info)
			value, err := adaptor.ConvertOpenAIRequest(c, info, req)
			require.NoError(t, err)
			converted := value.(*dto.GeminiChatRequest)
			require.NotNil(t, converted.GenerationConfig.ThinkingConfig)
			assert.Equal(t, lo.ToPtr(0), converted.GenerationConfig.ThinkingConfig.ThinkingBudget)
			assert.Equal(t, lo.ToPtr(uint(256)), converted.GenerationConfig.MaxOutputTokens)
			body, err := common.Marshal(value)
			require.NoError(t, err)
			assert.True(t, gjson.GetBytes(body, "generationConfig.thinkingConfig.thinkingBudget").Exists(), "explicit zero must reach the upstream")
		})
	}
}

func TestDegradationClaudeReasoningOffRequest(t *testing.T) {
	settings := model_setting.GetClaudeSettings()
	oldEnabled := settings.ThinkingAdapterEnabled
	settings.ThinkingAdapterEnabled = true
	t.Cleanup(func() { settings.ThinkingAdapterEnabled = oldEnabled })
	for _, tc := range []struct {
		model   string
		apiType int
	}{
		{"claude-sonnet-4", constant.APITypeAnthropic},
		{"claude-sonnet-4-thinking", constant.APITypeAnthropic},
		{"claude-opus-4-6-high", constant.APITypeAnthropic},
		{"anthropic.claude-sonnet-4-thinking", constant.APITypeAws},
		{"claude-sonnet-4-thinking", constant.APITypeVertexAi},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.model, tc.apiType), func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{Model: tc.model, MaxTokens: lo.ToPtr(uint(256)), Temperature: lo.ToPtr(0.0), Messages: []dto.Message{{Role: "user", Content: "1+1?"}}}
			require.NoError(t, applyProbeReasoningParams(req, &degradation_setting.ModelProbeBinding{ReasoningEffort: "off"}, 256, tc.apiType))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: req.Model}, OriginModelName: req.Model}
			adaptor := relay.GetAdaptor(tc.apiType)
			adaptor.Init(info)
			value, err := adaptor.ConvertOpenAIRequest(c, info, req)
			require.NoError(t, err)
			body, err := common.Marshal(value)
			require.NoError(t, err)
			assert.Equal(t, "disabled", gjson.GetBytes(body, "thinking.type").String())
			assert.False(t, gjson.GetBytes(body, "thinking.budget_tokens").Exists())
			assert.False(t, gjson.GetBytes(body, "output_config").Exists())
			assert.Equal(t, int64(256), gjson.GetBytes(body, "max_tokens").Int())
			assert.False(t, gjson.GetBytes(body, "temperature").Exists())
		})
	}
}

func TestDegradationOpenRouterReasoningOffRequest(t *testing.T) {
	for _, modelName := range []string{"anthropic/claude-sonnet-4-thinking", "google/gemini-2.5-flash", "openai/gpt-5.1"} {
		t.Run(modelName, func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{Model: modelName, MaxTokens: lo.ToPtr(uint(256))}
			require.NoError(t, applyProbeReasoningParams(req, &degradation_setting.ModelProbeBinding{ReasoningEffort: "off"}, 256, constant.APITypeOpenRouter))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, UpstreamModelName: req.Model}, OriginModelName: req.Model}
			value, err := (&openai.Adaptor{}).ConvertOpenAIRequest(c, info, req)
			require.NoError(t, err)
			body, err := common.Marshal(value)
			require.NoError(t, err)
			enabled := gjson.GetBytes(body, "reasoning.enabled")
			assert.True(t, enabled.Exists())
			assert.False(t, enabled.Bool())
			assert.False(t, gjson.GetBytes(body, "reasoning.max_tokens").Exists())
			assert.False(t, gjson.GetBytes(body, "reasoning_effort").Exists())
			assert.Equal(t, int64(256), gjson.GetBytes(body, "max_tokens").Int())
		})
	}
}

func TestDegradationOpenAIReasoningOffRequest(t *testing.T) {
	for _, modelName := range []string{"gpt-5.1", "gpt-5.2", "gpt-5.1-high"} {
		t.Run(modelName, func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{Model: modelName, MaxTokens: lo.ToPtr(uint(256)), Temperature: lo.ToPtr(0.0)}
			require.NoError(t, applyProbeReasoningParams(req, &degradation_setting.ModelProbeBinding{ReasoningEffort: "off"}, 256, constant.APITypeOpenAI))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: req.Model}, OriginModelName: modelName}
			value, err := (&openai.Adaptor{}).ConvertOpenAIRequest(c, info, req)
			require.NoError(t, err)
			body, err := common.Marshal(value)
			require.NoError(t, err)
			assert.Equal(t, "none", gjson.GetBytes(body, "reasoning_effort").String())
			assert.Equal(t, int64(256), gjson.GetBytes(body, "max_completion_tokens").Int())
			assert.False(t, gjson.GetBytes(body, "max_tokens").Exists())
		})
	}
}

func TestDegradationOpenAIExplicitReasoningOverridesModelSuffix(t *testing.T) {
	for _, tc := range []struct {
		model, effort, wantModel, wantEffort string
		wantLimit                            int64
	}{
		{"gpt-5.1-high", "low", "gpt-5.1", "low", 1280},
		{"gpt-5.1-low", "high", "gpt-5.1", "high", 4352},
		{"o3-high", "medium", "o3", "medium", 2304},
		{"gpt-5.1-high", "", "gpt-5.1", "high", 256},
		{"custom-high", "low", "custom-high", "low", 1280},
	} {
		t.Run(tc.model+"/"+tc.effort, func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{Model: tc.model, MaxTokens: lo.ToPtr(uint(256))}
			require.NoError(t, applyProbeReasoningParams(req, &degradation_setting.ModelProbeBinding{ReasoningEffort: tc.effort}, 256, constant.APITypeOpenAI))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: req.Model}, OriginModelName: tc.model}
			value, err := (&openai.Adaptor{}).ConvertOpenAIRequest(c, info, req)
			require.NoError(t, err)
			body, err := common.Marshal(value)
			require.NoError(t, err)
			assert.Equal(t, tc.wantModel, gjson.GetBytes(body, "model").String())
			assert.Equal(t, tc.wantEffort, gjson.GetBytes(body, "reasoning_effort").String())
			assert.Equal(t, tc.wantLimit, gjson.GetBytes(body, "max_completion_tokens").Int())
			assert.False(t, gjson.GetBytes(body, "max_tokens").Exists())
		})
	}
}

func TestDegradationRejectsUnsupportedReasoningOff(t *testing.T) {
	for _, tc := range []struct {
		model   string
		apiType int
	}{
		{"gemini-2.5-pro", constant.APITypeGemini},
		{"gemini-3-flash-preview", constant.APITypeGemini},
		{"gemini-3.1-pro-preview", constant.APITypeVertexAi},
		{"google/gemini-2.5-pro", constant.APITypeOpenRouter},
		{"gpt-5", constant.APITypeOpenAI},
		{"gpt-5-mini", constant.APITypeOpenAI},
		{"gpt-5.2-pro", constant.APITypeOpenAI},
		{"o3", constant.APITypeOpenAI},
	} {
		t.Run(tc.model, func(t *testing.T) {
			err := applyProbeReasoningParams(&dto.GeneralOpenAIRequest{Model: tc.model}, &degradation_setting.ModelProbeBinding{ReasoningEffort: "off"}, 256, tc.apiType)
			require.ErrorContains(t, err, "cannot disable thinking")
		})
	}
}

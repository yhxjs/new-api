package controller

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/degradation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyProbeReasoningParams(t *testing.T) {
	for _, tc := range []struct {
		effort          string
		completionLimit uint
	}{
		{"", 256}, {"off", 256}, {"low", 1280}, {"medium", 2304}, {"high", 4352}, {"custom", 0}, {"minimal", 0},
	} {
		effort := tc.effort
		t.Run(effort, func(t *testing.T) {
			req := &dto.GeneralOpenAIRequest{Model: "gpt-5.1", MaxTokens: uintPtr(256)}
			err := applyProbeReasoningParams(req, &degradation_setting.ModelProbeBinding{ReasoningEffort: effort, ThinkingBudget: 2048}, 256, constant.APITypeOpenAI)
			if effort == "custom" || effort == "minimal" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if effort == "" {
				assert.Empty(t, req.ReasoningEffort)
				assert.Nil(t, req.MaxCompletionTokens)
				assert.Equal(t, uintPtr(256), req.MaxTokens)
				return
			}
			wantEffort := effort
			if effort == "off" {
				wantEffort = "none"
			}
			assert.Equal(t, wantEffort, req.ReasoningEffort)
			assert.Nil(t, req.MaxTokens)
			assert.Equal(t, uintPtr(tc.completionLimit), req.MaxCompletionTokens)
		})
	}
}

func uintPtr(v uint) *uint { return &v }

func TestEvaluateProbeOutput(t *testing.T) {
	probe := degradation_setting.ProbeConfig{
		ID:            "test_probe",
		Name:          "Test Probe",
		Keywords:      []string{"579"},
		ExcludeWords:  []string{"抱歉", "无法回答"},
		MatchRule:     "any",
		CaseSensitive: false,
	}

	// Case 1: 准确包含关键词
	resp1 := []byte(`{"choices":[{"message":{"content":"结果是 579。"}}]}`)
	res1 := evaluateProbeOutput(resp1, probe, 100)
	assert.Equal(t, model.DegradationStatusPassed, res1.Status)
	assert.Empty(t, res1.Detail)

	// Case 2: 未包含关键词（答案错误/降智）
	resp2 := []byte(`{"choices":[{"message":{"content":"结果是 580。"}}]}`)
	res2 := evaluateProbeOutput(resp2, probe, 100)
	assert.Equal(t, model.DegradationStatusMismatched, res2.Status)
	assert.Contains(t, res2.Detail, "580")

	// Case 3: 命中排除词（即回答中带有套话或拒答）
	resp3 := []byte(`{"choices":[{"message":{"content":"抱歉，我不能提供这个计算。"}}]}`)
	res3 := evaluateProbeOutput(resp3, probe, 100)
	assert.Equal(t, model.DegradationStatusMismatched, res3.Status)
	assert.Contains(t, res3.Detail, "hit exclude word")

	// Case 4: 空输出
	resp4 := []byte(`{"choices":[{"message":{"content":""}}]}`)
	res4 := evaluateProbeOutput(resp4, probe, 100)
	assert.Equal(t, model.DegradationStatusMismatched, res4.Status)
	assert.Contains(t, res4.Detail, "empty response")

	// Case 5: regex 规则在 CaseSensitive=false 时忽略大小写
	regexProbe := probe
	regexProbe.MatchRule = "regex"
	regexProbe.Keywords = []string{"result .*579"}
	resp5 := []byte(`{"choices":[{"message":{"content":"The RESULT is 579."}}]}`)
	res5 := evaluateProbeOutput(resp5, regexProbe, 100)
	assert.Equal(t, model.DegradationStatusPassed, res5.Status)

	// Case 6: regex 规则大小写敏感时不匹配
	regexProbe.CaseSensitive = true
	res6 := evaluateProbeOutput(resp5, regexProbe, 100)
	assert.Equal(t, model.DegradationStatusMismatched, res6.Status)
}

func TestEvaluateProbeOutputSkipsBlankRegex(t *testing.T) {
	for _, blank := range []string{"", " \t\n"} {
		t.Run(blank, func(t *testing.T) {
			probes := []degradation_setting.ProbeConfig{{
				ID: "math", Name: "Math", Prompt: "1+1?", MaxTokens: 256,
				MatchRule: "regex", Keywords: []string{"^2$", blank},
			}}
			encoded, err := common.Marshal(probes)
			require.NoError(t, err)
			require.NoError(t, degradation_setting.ValidateProbesJSON(string(encoded)))
			for _, tc := range []struct {
				content string
				status  int
			}{{"wrong answer", model.DegradationStatusMismatched}, {"2", model.DegradationStatusPassed}} {
				body, err := common.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": tc.content}}}})
				require.NoError(t, err)
				assert.Equal(t, tc.status, evaluateProbeOutput(body, probes[0], 1).Status)
			}
		})
	}
}

func TestEvaluateProbeOutputReportsInvalidRegexError(t *testing.T) {
	probe := degradation_setting.ProbeConfig{
		ID: "invalid-regex", Name: "Regex Test", Prompt: "test", MaxTokens: 256,
		MatchRule: "regex", Keywords: []string{"[unclosed"},
	}
	body, err := common.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "some output"}}}})
	require.NoError(t, err)
	result := evaluateProbeOutput(body, probe, 100)
	assert.Equal(t, model.DegradationStatusError, result.Status)
	assert.Contains(t, result.Detail, "invalid regex keyword")
}

func TestCoerceTestUsageRejectsNegativeValues(t *testing.T) {
	_, err := coerceTestUsage(&dto.Usage{PromptTokens: -1}, false, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt_tokens")

	_, err = coerceTestUsage(&dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{CachedTokens: -1},
	}, false, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cached_tokens")

	usage, err := coerceTestUsage(nil, true, 10)
	require.NoError(t, err)
	assert.Equal(t, 10, usage.PromptTokens)
}

func TestCoerceTestUsageNumericBounds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		usage    any
		stream   bool
		estimate int
		wantErr  bool
	}{
		{name: "typed nil", usage: (*dto.Usage)(nil), wantErr: true},
		{name: "oversized prompt", usage: &dto.Usage{PromptTokens: common.MaxQuota + 1}, wantErr: true},
		{name: "negative response input detail", usage: &dto.Usage{InputTokensDetails: &dto.InputTokenDetails{CachedTokens: -1}}, wantErr: true},
		{name: "oversized reasoning", usage: &dto.Usage{CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: common.MaxQuota + 1}}, wantErr: true},
		{name: "negative estimate", stream: true, estimate: -1, wantErr: true},
		{name: "oversized estimate", stream: true, estimate: common.MaxQuota + 1, wantErr: true},
		{name: "response usage takes precedence over estimate", usage: &dto.Usage{PromptTokens: 10}, estimate: -1},
		{name: "valid cache overlap", usage: &dto.Usage{PromptTokens: 10, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 10, CacheWriteTokens: 10}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage, err := coerceTestUsage(tc.usage, tc.stream, tc.estimate)
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, usage)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, usage)
		})
	}
}

func TestTruncateDetailKeepsValidUTF8(t *testing.T) {
	long := strings.Repeat("降智", 400) // 800 rune，按字节远超 500
	truncated := truncateDetail(long)
	assert.True(t, strings.HasSuffix(truncated, "..."))
	body := strings.TrimSuffix(truncated, "...")
	runes := []rune(body)
	assert.Len(t, runes, 500)
	assert.Equal(t, "降智", string(runes[498:]))
}

func TestMaskedNodeName(t *testing.T) {
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "degradation-node-test-secret"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })

	// Same channel ID always maps to the same masked node.
	assert.Equal(t, maskedNodeName(1), maskedNodeName(1))

	name := maskedNodeName(1)
	assert.Regexp(t, `^Node-[0-9A-F]{12}$`, name)

	// IDs that collide under the legacy mod-26 bucket must stay distinct.
	assert.NotEqual(t, maskedNodeName(1), maskedNodeName(27))
	assert.NotEqual(t, maskedNodeName(2), maskedNodeName(28))
}

func TestMaskedNodeNameChangesWithServerSecret(t *testing.T) {
	previousSecret := common.CryptoSecret
	t.Cleanup(func() { common.CryptoSecret = previousSecret })
	common.CryptoSecret = "degradation-node-first-test-secret"
	name := maskedNodeName(98004)

	common.CryptoSecret = "degradation-node-second-test-secret"
	assert.NotEqual(t, name, maskedNodeName(98004), "public labels must depend on the private server secret")

	common.CryptoSecret = "degradation-node-first-test-secret"
	assert.Equal(t, name, maskedNodeName(98004), "nodes with the same secret must generate stable labels")
}

func TestApplySlotRow(t *testing.T) {
	now := time.Now()
	rows := []model.DegradationSlotRow{
		{
			Status:    model.DegradationStatusPassed,
			CreatedAt: now.Add(-1 * time.Hour),
		},
		{
			Status:    model.DegradationStatusMismatched,
			CreatedAt: now.Add(-2 * time.Hour),
		},
		{
			Status:    model.DegradationStatusError,
			CreatedAt: now.Add(-5 * time.Hour),
		},
	}

	var slots [24]int
	for _, row := range rows {
		applySlotRow(&slots, row.Status, row.CreatedAt, now)
	}
	assert.Equal(t, model.DegradationStatusPassed, slots[22])     // 1 hour ago (index 23 - 1 = 22)
	assert.Equal(t, model.DegradationStatusMismatched, slots[21]) // 2 hours ago (index 23 - 2 = 21)
	assert.Equal(t, model.DegradationStatusError, slots[18])      // 5 hours ago (index 23 - 5 = 18)
	assert.Equal(t, 0, slots[0])                                  // 23 hours ago (empty)

	// Within one hour the most severe outcome wins: error/timeout > mismatch.
	var severity [24]int
	applySlotRow(&severity, model.DegradationStatusMismatched, now.Add(-3*time.Hour), now)
	applySlotRow(&severity, model.DegradationStatusTimeout, now.Add(-3*time.Hour), now)
	assert.Equal(t, model.DegradationStatusTimeout, severity[20])

	// A later mismatch must not downgrade an already recorded timeout.
	applySlotRow(&severity, model.DegradationStatusMismatched, now.Add(-3*time.Hour), now)
	assert.Equal(t, model.DegradationStatusTimeout, severity[20])

	// Rows outside the 24h window are ignored.
	var window [24]int
	applySlotRow(&window, model.DegradationStatusError, now.Add(-25*time.Hour), now)
	applySlotRow(&window, model.DegradationStatusError, now.Add(1*time.Hour), now)
	assert.Equal(t, [24]int{}, window)
}

func TestApplicableProbesForBinding(t *testing.T) {
	setting := degradation_setting.DegradationSetting{
		Probes: []degradation_setting.ProbeConfig{
			{ID: "p1", Name: "P1"},
			{ID: "p2", Name: "P2"},
		},
	}
	probesMap := map[string]degradation_setting.ProbeConfig{
		"p1": setting.Probes[0],
		"p2": setting.Probes[1],
	}

	// 无绑定回退全部探针
	assert.Equal(t, setting.Probes, applicableProbesForBinding(setting, probesMap, nil))

	// 空绑定 probe_ids 同样回退全部探针
	empty := &degradation_setting.ModelProbeBinding{ModelName: "m", Enabled: true}
	assert.Equal(t, setting.Probes, applicableProbesForBinding(setting, probesMap, empty))

	// 显式 probe_ids 优先，未知 id 丢弃
	bound := &degradation_setting.ModelProbeBinding{ModelName: "m", ProbeIDs: []string{"p2", "ghost"}}
	assert.Equal(t, []degradation_setting.ProbeConfig{setting.Probes[1]}, applicableProbesForBinding(setting, probesMap, bound))

	// 全部 id 未知时结果为空（执行端跳过、概览不展示）
	ghost := &degradation_setting.ModelProbeBinding{ModelName: "m", ProbeIDs: []string{"ghost"}}
	assert.Empty(t, applicableProbesForBinding(setting, probesMap, ghost))
}

func TestEvaluateProbeOutputMatchWindow(t *testing.T) {
	probe := degradation_setting.ProbeConfig{
		ID:        "window_probe",
		Name:      "Window Probe",
		Keywords:  []string{"579"},
		MatchRule: "any",
	}

	// 关键词在匹配窗口内正常命中
	within := []byte(`{"choices":[{"message":{"content":"` + strings.Repeat("字", 4000) + `结果是 579。"}}]}`)
	assert.Equal(t, model.DegradationStatusPassed, evaluateProbeOutput(within, probe, 1).Status)

	// 关键词落在窗口（4096 rune）之外不参与匹配
	past := []byte(`{"choices":[{"message":{"content":"` + strings.Repeat("字", 4096) + `结果是 579。"}}]}`)
	assert.Equal(t, model.DegradationStatusMismatched, evaluateProbeOutput(past, probe, 1).Status)
}

func TestEvaluateProbeOutputAllBlankKeywords(t *testing.T) {
	// 保存校验会拦截全空白关键词；这里验证匹配器自身在 "all" 规则下
	// 遇到全空白关键词时判为不匹配，而不是无条件通过。
	probe := degradation_setting.ProbeConfig{
		ID:        "blank_probe",
		Name:      "Blank Probe",
		Keywords:  []string{"  ", ""},
		MatchRule: "all",
	}
	resp := []byte(`{"choices":[{"message":{"content":"anything"}}]}`)
	res := evaluateProbeOutput(resp, probe, 100)
	assert.Equal(t, model.DegradationStatusMismatched, res.Status)
}

func TestEvaluateProbeOutputUsageFallback(t *testing.T) {
	probe := degradation_setting.ProbeConfig{
		ID:        "probe_usage",
		Name:      "Usage Fallback Probe",
		Keywords:  []string{"passed"},
		MatchRule: "any",
	}
	resp := []byte(`{"choices":[{"message":{"content":"passed"}}]}`)
	usage := &dto.Usage{
		CompletionTokens: 42,
		CompletionTokenDetails: dto.OutputTokenDetails{
			ReasoningTokens: 18,
		},
	}
	res := evaluateProbeOutput(resp, probe, 100, usage)
	assert.Equal(t, model.DegradationStatusPassed, res.Status)
	assert.Equal(t, 42, res.TokenCount)
	assert.Equal(t, 18, res.ReasoningTokens)
}

func TestDegradationRequestCoveredByActive(t *testing.T) {
	fullRound := &model.SystemTask{Payload: ""}
	marshalPayload := func(t *testing.T, filter degradationTargetFilter) string {
		data, err := common.Marshal(filter)
		require.NoError(t, err)
		return string(data)
	}

	tests := []struct {
		name        string
		activeTask  *model.SystemTask
		request     degradationTargetFilter
		wantCovered bool
	}{
		{
			// 全轮任务可合并另一个全轮请求。
			name:        "full round covers full round",
			activeTask:  fullRound,
			request:     degradationTargetFilter{},
			wantCovered: true,
		},
		{
			name:        "full round does not claim an unverified target",
			activeTask:  fullRound,
			request:     degradationTargetFilter{ChannelID: 1, ModelName: "gpt-4o"},
			wantCovered: false,
		},
		{
			name:        "single target covers itself",
			activeTask:  &model.SystemTask{Payload: marshalPayload(t, degradationTargetFilter{ChannelID: 1, ModelName: "gpt-4o"})},
			request:     degradationTargetFilter{ChannelID: 1, ModelName: "gpt-4o"},
			wantCovered: true,
		},
		{
			name:        "single target does not cover full round",
			activeTask:  &model.SystemTask{Payload: marshalPayload(t, degradationTargetFilter{ChannelID: 1, ModelName: "gpt-4o"})},
			request:     degradationTargetFilter{},
			wantCovered: false,
		},
		{
			name:        "single target does not cover other model",
			activeTask:  &model.SystemTask{Payload: marshalPayload(t, degradationTargetFilter{ChannelID: 1, ModelName: "gpt-4o"})},
			request:     degradationTargetFilter{ChannelID: 1, ModelName: "claude-3-7-sonnet"},
			wantCovered: false,
		},
		{
			name:        "single target does not cover other channel",
			activeTask:  &model.SystemTask{Payload: marshalPayload(t, degradationTargetFilter{ChannelID: 1, ModelName: "gpt-4o"})},
			request:     degradationTargetFilter{ChannelID: 2, ModelName: "gpt-4o"},
			wantCovered: false,
		},
		{
			// payload 解码失败时保守判定为不覆盖，请求会被拒绝而不是静默丢失
			name:        "corrupt payload is not covered",
			activeTask:  &model.SystemTask{Payload: "{not-json"},
			request:     degradationTargetFilter{},
			wantCovered: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantCovered, degradationRequestCoveredByActive(tt.activeTask, tt.request))
		})
	}
}

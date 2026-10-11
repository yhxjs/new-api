package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	openaichannel "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/degradation_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/tidwall/gjson"
)

type DegradationProbeResult struct {
	Status          int
	LatencyMs       int
	TokenCount      int
	ReasoningTokens int
	Detail          string
}

// presetReasoningHeadroom extends the completion budget for preset reasoning
// efforts: their thinking tokens share the completion limit, so without extra
// headroom the reasoning swallows the probe's entire visible output and every
// round reports a false mismatch. custom bindings carry their own budget.
var presetReasoningHeadroom = map[string]int{
	"low":    1024,
	"medium": 2048,
	"high":   4096,
}

// applyProbeReasoningParams reserves visible output in addition to the actual
// thinking budget and maps it to the provider's supported protocol fields.
func applyProbeReasoningParams(req *dto.GeneralOpenAIRequest, binding *degradation_setting.ModelProbeBinding, maxTokens, apiType int) error {
	isClaude := apiType == constant.APITypeAnthropic ||
		(apiType == constant.APITypeAws && strings.Contains(req.Model, "claude")) ||
		(apiType == constant.APITypeVertexAi && strings.HasPrefix(req.Model, "claude")) ||
		(apiType == constant.APITypeOpenRouter && strings.Contains(req.Model, "claude"))
	claudeModelName := req.Model
	if index := strings.Index(claudeModelName, "claude-"); index >= 0 {
		claudeModelName = claudeModelName[index:]
	}
	claudeModelName = strings.ReplaceAll(claudeModelName, ".", "-")
	adaptiveOnlyClaude := isClaude && (strings.HasPrefix(claudeModelName, "claude-opus-4-7") ||
		strings.HasPrefix(claudeModelName, "claude-opus-4-8"))
	if adaptiveOnlyClaude {
		// These models reject sampling fields even when thinking is inherited.
		req.Temperature = nil
		req.TopP = nil
		req.TopK = nil
	}
	if binding == nil {
		return nil
	}
	effort := binding.ReasoningEffort
	if effort == "" {
		return nil
	}
	if apiType == constant.APITypeDeepSeek {
		// Explicit probe settings take precedence over DeepSeek thinking aliases.
		req.Model, _, _, _ = reasoning.ParseDeepSeekV4ThinkingSuffix(req.Model)
	}
	isGemini := apiType == constant.APITypeGemini ||
		(apiType == constant.APITypeVertexAi && strings.HasPrefix(req.Model, "gemini"))
	baseClaudeModel, _, hasClaudeEffortSuffix := reasoning.TrimEffortSuffix(req.Model)
	adaptiveClaude := adaptiveOnlyClaude || (isClaude && hasClaudeEffortSuffix && strings.HasPrefix(claudeModelName, "claude-opus-4-6"))
	if isClaude && (adaptiveOnlyClaude || strings.HasPrefix(claudeModelName, "claude-opus-4-6")) {
		// Explicit probe settings take precedence over adaptive model aliases.
		req.Model = strings.TrimSuffix(baseClaudeModel, "-thinking")
	}
	if effort == "off" {
		modelName := req.Model[strings.LastIndex(req.Model, "/")+1:]
		if strings.HasPrefix(modelName, "gemini-2.5-pro") || strings.HasPrefix(modelName, "gemini-3") ||
			dto.IsOpenAIReasoningOModel(modelName) ||
			(dto.IsOpenAIGPT5Model(modelName) && (strings.Contains(modelName, "-pro") ||
				(!strings.HasPrefix(modelName, "gpt-5.") && !strings.HasPrefix(modelName, "gpt-5-chat")))) {
			return fmt.Errorf("model %s cannot disable thinking", req.Model)
		}
		switch {
		case isGemini:
			if !strings.HasPrefix(modelName, "gemini-2.5-flash") {
				if strings.Contains(modelName, "thinking") {
					return fmt.Errorf("model %s cannot disable thinking", req.Model)
				}
				return nil // Earlier non-thinking models need no thinking_config.
			}
			// The converter skips extra_body for this suffix; use the base model
			// so an explicit zero survives even with suffix adaptation disabled.
			req.Model = strings.TrimSuffix(req.Model, "-nothinking")
			extraBody, err := common.Marshal(map[string]any{"google": map[string]any{"thinking_config": map[string]any{"thinking_budget": 0}}})
			if err != nil {
				return err
			}
			req.ExtraBody = extraBody
		case isClaude || apiType == constant.APITypeOpenRouter:
			encoded, err := common.Marshal(map[string]any{"enabled": false})
			if err != nil {
				return err
			}
			req.Reasoning = encoded
		case apiType == constant.APITypeDeepSeek || apiType == constant.APITypeOllama:
			req.ReasoningEffort = "none"
			req.MaxTokens = lo.ToPtr(uint(maxTokens))
			req.MaxCompletionTokens = nil
		case apiType == constant.APITypeOpenAI:
			if strings.HasPrefix(modelName, "gpt-4") || strings.HasPrefix(modelName, "gpt-3.5") || strings.HasPrefix(modelName, "gpt-5-chat") {
				return nil
			}
			_, req.Model = reasoning.ParseOpenAIReasoningEffortFromModelSuffix(req.Model)
			req.ReasoningEffort = "none"
			req.MaxTokens = nil
			req.MaxCompletionTokens = lo.ToPtr(uint(maxTokens))
		default:
			return fmt.Errorf("reasoning off is not supported by this provider")
		}
		req.Temperature = nil
		return nil
	}
	budget, preset := presetReasoningHeadroom[effort]
	if effort == "custom" {
		budget = binding.ThinkingBudget
	} else if !preset {
		return fmt.Errorf("unsupported reasoning effort: %s", effort)
	}
	if budget <= 0 || budget > degradation_setting.MaxThinkingBudget {
		return fmt.Errorf("thinking budget must be between 1 and %d", degradation_setting.MaxThinkingBudget)
	}
	if adaptiveOnlyClaude && effort == "custom" {
		return fmt.Errorf("model %s supports adaptive thinking instead of custom token budgets", req.Model)
	}
	switch {
	case isClaude && apiType != constant.APITypeOpenRouter:
		if adaptiveClaude && effort != "custom" {
			encoded, err := common.Marshal(map[string]any{"effort": effort})
			if err != nil {
				return err
			}
			req.Reasoning = encoded
			req.Temperature = nil
			req.TopP = nil
			req.TopK = nil
			req.MaxTokens = lo.ToPtr(uint(maxTokens + budget))
			req.MaxCompletionTokens = nil
			return nil
		}
		if effort == "low" {
			budget = 1280 // Matches the Claude converter's low preset.
		}
		if budget < 1024 {
			return fmt.Errorf("Claude thinking budget must be at least 1024")
		}
		reasoning, err := common.Marshal(map[string]any{"max_tokens": budget})
		if err != nil {
			return err
		}
		req.Reasoning = reasoning
		req.Temperature = lo.ToPtr(1.0)
	case isGemini:
		req.Model = strings.TrimSuffix(req.Model, "-nothinking")
		thinking := map[string]any{"thinking_budget": budget}
		if strings.HasPrefix(req.Model, "gemini-3") {
			if effort == "custom" {
				return fmt.Errorf("Gemini 3 supports thinking levels instead of custom token budgets")
			}
			if strings.HasPrefix(req.Model, "gemini-3-pro") && effort == "medium" {
				return fmt.Errorf("Gemini 3 Pro supports low or high thinking levels")
			}
			thinking = map[string]any{"thinking_level": effort}
		} else {
			minBudget := 1
			maxBudget := 24576
			if strings.HasPrefix(req.Model, "gemini-2.5-pro") {
				minBudget = 128
				maxBudget = 32768
			} else if strings.HasPrefix(req.Model, "gemini-2.5-flash-lite") {
				minBudget = 512
			}
			if budget < minBudget || budget > maxBudget {
				return fmt.Errorf("Gemini thinking budget must be between %d and %d", minBudget, maxBudget)
			}
		}
		extraBody, err := common.Marshal(map[string]any{"google": map[string]any{"thinking_config": thinking}})
		if err != nil {
			return err
		}
		req.ExtraBody = extraBody
		req.Temperature = nil
	case apiType == constant.APITypeOpenRouter:
		reasoning := map[string]any{"enabled": true, "effort": effort}
		if effort == "custom" {
			reasoning = map[string]any{"enabled": true, "max_tokens": budget}
		}
		encoded, err := common.Marshal(reasoning)
		if err != nil {
			return err
		}
		req.Reasoning = encoded
		req.Temperature = nil
	default:
		if effort == "custom" {
			return fmt.Errorf("custom thinking budget is not supported by this provider")
		}
		// The OpenAI adaptor derives effort from known reasoning-model suffixes.
		// Remove that suffix so the explicit probe binding keeps precedence.
		if dto.IsOpenAIReasoningOModel(req.Model) || dto.IsOpenAIGPT5Model(req.Model) {
			_, req.Model = reasoning.ParseOpenAIReasoningEffortFromModelSuffix(req.Model)
		}
		req.ReasoningEffort = effort
		req.Temperature = nil
		if apiType == constant.APITypeDeepSeek {
			req.MaxTokens = lo.ToPtr(uint(maxTokens + budget))
			req.MaxCompletionTokens = nil
		} else {
			req.MaxTokens = nil
			req.MaxCompletionTokens = lo.ToPtr(uint(maxTokens + budget))
		}
		return nil
	}
	req.MaxTokens = lo.ToPtr(uint(maxTokens + budget))
	req.MaxCompletionTokens = nil
	return nil
}

// probeMatchWindowRunes caps the content fed to keyword/regex matching. Go's
// regexp (RE2) runs in linear time, but its cost still scales with input
// length and there is no per-match deadline, so an expensive admin pattern
// over unbounded output would burn CPU every round; probe outputs are short
// by design (max_tokens is capped), so the tail beyond this window cannot be
// a meaningful match target anyway.
const probeMatchWindowRunes = 4096

func ExecuteDegradationProbe(
	ctx context.Context,
	probeUserID int,
	channel *model.Channel,
	modelName string,
	probe degradation_setting.ProbeConfig,
	binding *degradation_setting.ModelProbeBinding,
) *DegradationProbeResult {
	if ctx == nil {
		ctx = context.Background()
	}
	startTime := time.Now()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	c.Request = httptest.NewRequestWithContext(ctx, "POST", "/v1/chat/completions", bytes.NewBuffer([]byte("{}")))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("channel", channel.Type)
	c.Set("base_url", channel.GetBaseURL())
	// 与渠道测试一致地写入用户缓存：HandleGroupRatio 需要 user_group 才能应用
	// 用户分组专属比例，ModelPriceHelper 依赖 UserSetting.AcceptUnsetRatioModel，
	// RecordConsumeLog 依赖 username 键；缺失时未配倍率模型会误报 error。
	cache, cacheErr := model.GetUserCache(probeUserID)
	if cacheErr != nil {
		return &DegradationProbeResult{
			Status:    model.DegradationStatusError,
			LatencyMs: int(time.Since(startTime).Milliseconds()),
			Detail:    truncateDetail("load probe user cache failed: " + cacheErr.Error()),
		}
	}
	cache.WriteContext(c)
	c.Set("id", probeUserID)
	group, _ := model.GetUserGroup(probeUserID, false)
	c.Set("group", group)

	if setupErr := middleware.SetupContextForSelectedChannel(c, channel, modelName); setupErr != nil {
		return &DegradationProbeResult{
			Status:    model.DegradationStatusError,
			LatencyMs: int(time.Since(startTime).Milliseconds()),
			Detail:    truncateDetail(setupErr.Error()),
		}
	}

	var messages []dto.Message
	if strings.TrimSpace(probe.SystemPrompt) != "" {
		messages = append(messages, dto.Message{
			Role:    "system",
			Content: probe.SystemPrompt,
		})
	}
	messages = append(messages, dto.Message{
		Role:    "user",
		Content: probe.Prompt,
	})

	maxTokens := probe.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 256
	}
	if maxTokens > degradation_setting.MaxProbeMaxTokens {
		maxTokens = degradation_setting.MaxProbeMaxTokens
	}

	temp := 0.0
	if probe.Temperature != nil {
		temp = *probe.Temperature
	}

	req := &dto.GeneralOpenAIRequest{
		Model:       modelName,
		Stream:      lo.ToPtr(false),
		Messages:    messages,
		MaxTokens:   lo.ToPtr(uint(maxTokens)),
		Temperature: lo.ToPtr(temp),
	}

	info, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAI, req, nil)
	if err != nil {
		return &DegradationProbeResult{
			Status:    model.DegradationStatusError,
			LatencyMs: int(time.Since(startTime).Milliseconds()),
			Detail:    truncateDetail("gen relay info failed: " + err.Error()),
		}
	}

	info.IsChannelTest = true
	info.DisablePing = true
	info.InitChannelMeta(c)

	if err = helper.ModelMappedHelper(c, info, req); err != nil {
		return &DegradationProbeResult{
			Status:    model.DegradationStatusError,
			LatencyMs: int(time.Since(startTime).Milliseconds()),
			Detail:    truncateDetail("model mapped failed: " + err.Error()),
		}
	}
	req.SetModelName(info.UpstreamModelName)
	apiType, _ := common.ChannelType2APIType(channel.Type)
	if err = applyProbeReasoningParams(req, binding, maxTokens, apiType); err != nil {
		return &DegradationProbeResult{
			Status:    model.DegradationStatusError,
			LatencyMs: int(time.Since(startTime).Milliseconds()),
			Detail:    truncateDetail("invalid reasoning configuration: " + err.Error()),
		}
	}
	info.UpstreamModelName = req.Model
	if binding != nil {
		info.SetReasoningEffort(binding.ReasoningEffort)
	}
	var request dto.Request = req
	globalSettings := model_setting.GetGlobalSettings()
	useResponses := channel.Type == constant.ChannelTypeCodex || strings.Contains(strings.ToLower(req.Model), "codex") ||
		(!globalSettings.PassThroughRequestEnabled && !info.ChannelSetting.PassThroughBodyEnabled &&
			service.ShouldChatCompletionsUseResponsesGlobal(channel.Id, channel.Type, info.OriginModelName))
	if useResponses {
		if dto.IsOpenAIReasoningOModel(req.Model) || dto.IsOpenAIGPT5Model(req.Model) {
			req.Temperature = nil
		}
		responsesReq, convertErr := relayconvert.ChatCompletionsRequestToResponsesRequest(req)
		if convertErr != nil {
			return &DegradationProbeResult{
				Status:    model.DegradationStatusError,
				LatencyMs: int(time.Since(startTime).Milliseconds()),
				Detail:    truncateDetail("convert responses request failed: " + convertErr.Error()),
			}
		}
		responsesReq.Stream = lo.ToPtr(shouldUseStreamForAutomaticChannelTest(channel))
		request = responsesReq
		info.IsStream = *responsesReq.Stream
		info.RelayMode = relayconstant.RelayModeResponses
		info.RequestURLPath = "/v1/responses"
		c.Request.URL.Path = "/v1/responses"
	}
	if err = attachTestBillingRequestInput(info, request); err != nil {
		return &DegradationProbeResult{
			Status:    model.DegradationStatusError,
			LatencyMs: int(time.Since(startTime).Milliseconds()),
			Detail:    truncateDetail("marshal request failed: " + err.Error()),
		}
	}

	priceData, err := helper.ModelPriceHelper(c, info, 0, request.GetTokenCountMeta())
	if err != nil {
		return &DegradationProbeResult{
			Status:    model.DegradationStatusError,
			LatencyMs: int(time.Since(startTime).Milliseconds()),
			Detail:    truncateDetail("model price failed: " + err.Error()),
		}
	}

	adaptor := relay.GetAdaptor(apiType)
	if adaptor == nil {
		return &DegradationProbeResult{
			Status:    model.DegradationStatusError,
			LatencyMs: int(time.Since(startTime).Milliseconds()),
			Detail:    truncateDetail(fmt.Sprintf("adaptor is nil for api type: %d", apiType)),
		}
	}
	adaptor.Init(info)

	var convertedRequest any
	if responsesReq, ok := request.(*dto.OpenAIResponsesRequest); ok {
		convertedRequest, err = adaptor.ConvertOpenAIResponsesRequest(c, info, *responsesReq)
	} else {
		convertedRequest, err = adaptor.ConvertOpenAIRequest(c, info, req)
	}
	if err != nil {
		return &DegradationProbeResult{
			Status:    model.DegradationStatusError,
			LatencyMs: int(time.Since(startTime).Milliseconds()),
			Detail:    truncateDetail("convert request failed: " + err.Error()),
		}
	}

	convertedReqBytes, err := common.Marshal(convertedRequest)
	if err != nil {
		return &DegradationProbeResult{
			Status:    model.DegradationStatusError,
			LatencyMs: int(time.Since(startTime).Milliseconds()),
			Detail:    truncateDetail("marshal converted request failed: " + err.Error()),
		}
	}

	if len(info.ParamOverride) > 0 {
		convertedReqBytes, err = relaycommon.ApplyParamOverrideWithRelayInfo(convertedReqBytes, info)
		if err != nil {
			return &DegradationProbeResult{
				Status:    model.DegradationStatusError,
				LatencyMs: int(time.Since(startTime).Milliseconds()),
				Detail:    truncateDetail("param override failed: " + err.Error()),
			}
		}
	}

	requestBody := bytes.NewBuffer(convertedReqBytes)
	c.Request.Body = io.NopCloser(bytes.NewBuffer(convertedReqBytes))
	rawResp, err := adaptor.DoRequest(c, info, requestBody)
	latency := int(time.Since(startTime).Milliseconds())
	if err != nil {
		status := model.DegradationStatusError
		var netErr net.Error
		if ctx.Err() == context.DeadlineExceeded ||
			errors.Is(err, context.DeadlineExceeded) ||
			(errors.As(err, &netErr) && netErr.Timeout()) {
			status = model.DegradationStatusTimeout
		}
		return &DegradationProbeResult{
			Status:    status,
			LatencyMs: latency,
			Detail:    truncateDetail(err.Error()),
		}
	}

	var httpResp *http.Response
	if rawResp != nil {
		if hr, ok := rawResp.(*http.Response); ok {
			httpResp = hr
		}
	}

	isAwsSDKRequest := apiType == constant.APITypeAws && info.ChannelOtherSettings.AwsKeyType != dto.AwsKeyTypeApiKey
	if httpResp == nil && !isAwsSDKRequest {
		return &DegradationProbeResult{
			Status:    model.DegradationStatusError,
			LatencyMs: latency,
			Detail:    "upstream response is nil",
		}
	}

	if httpResp != nil {
		defer func() {
			if httpResp.Body != nil {
				_ = httpResp.Body.Close()
			}
		}()

		if httpResp.StatusCode != http.StatusOK {
			relayErr := service.RelayErrorHandler(c.Request.Context(), httpResp, true)
			detail := fmt.Sprintf("HTTP %d", httpResp.StatusCode)
			if relayErr != nil {
				detail = fmt.Sprintf("HTTP %d: %v", httpResp.StatusCode, relayErr)
			}
			status := model.DegradationStatusError
			if ctx.Err() == context.DeadlineExceeded {
				status = model.DegradationStatusTimeout
			}
			return &DegradationProbeResult{
				Status:    status,
				LatencyMs: int(time.Since(startTime).Milliseconds()),
				Detail:    truncateDetail(detail),
			}
		}
	}

	var usageAny any
	var respErr *types.NewAPIError
	if useResponses {
		if strings.Contains(strings.ToLower(httpResp.Header.Get("Content-Type")), "text/event-stream") {
			usageAny, respErr = openaichannel.OaiResponsesToChatBufferedStreamHandler(c, info, httpResp)
		} else {
			usageAny, respErr = openaichannel.OaiResponsesToChatHandler(c, info, httpResp)
		}
	} else {
		usageAny, respErr = adaptor.DoResponse(c, httpResp, info)
	}
	latency = int(time.Since(startTime).Milliseconds())
	if respErr != nil {
		status := model.DegradationStatusError
		if ctx.Err() == context.DeadlineExceeded {
			status = model.DegradationStatusTimeout
		}
		return &DegradationProbeResult{
			Status:    status,
			LatencyMs: latency,
			Detail:    truncateDetail(respErr.Error()),
		}
	}

	usage, usageErr := coerceTestUsage(usageAny, info.IsStream, info.GetEstimatePromptTokens())
	if usageErr != nil {
		return &DegradationProbeResult{
			Status:    model.DegradationStatusError,
			LatencyMs: latency,
			Detail:    truncateDetail("invalid usage: " + usageErr.Error()),
		}
	}

	// 与渠道测试一致：计算应计费用并写入消费日志供审计，但不扣减用户余额。
	quota, tieredResult := settleTestQuota(info, priceData, usage)
	other := buildTestLogOther(c, info, priceData, usage, tieredResult)
	model.RecordConsumeLog(c, probeUserID, model.RecordConsumeLogParams{
		ChannelId:        channel.Id,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		ModelName:        info.OriginModelName,
		TokenName:        "模型巡检",
		Quota:            quota,
		Content:          "模型巡检",
		UseTimeSeconds:   latency / 1000,
		IsStream:         info.IsStream,
		Group:            info.UsingGroup,
		Other:            other,
	})

	respBytes := w.Body.Bytes()
	return evaluateProbeOutput(respBytes, probe, latency, usage)
}

func evaluateProbeOutput(respBody []byte, probe degradation_setting.ProbeConfig, latencyMs int, usages ...*dto.Usage) *DegradationProbeResult {
	root := gjson.ParseBytes(respBody)

	content := root.Get("choices.0.message.content").String()
	if content == "" {
		content = root.Get("choices.0.text").String()
	}

	var usage *dto.Usage
	if len(usages) > 0 {
		usage = usages[0]
	}

	tokenCount := int(root.Get("usage.completion_tokens").Int())
	if tokenCount == 0 && usage != nil {
		tokenCount = usage.CompletionTokens
	}
	reasoningTokens := int(root.Get("usage.completion_tokens_details.reasoning_tokens").Int())
	if reasoningTokens == 0 {
		reasoningTokens = int(root.Get("usage.reasoning_tokens").Int())
	}
	if reasoningTokens == 0 && usage != nil {
		reasoningTokens = usage.CompletionTokenDetails.ReasoningTokens
	}

	contentTrimmed := strings.TrimSpace(content)
	if contentTrimmed == "" {
		return &DegradationProbeResult{
			Status:          model.DegradationStatusMismatched,
			LatencyMs:       latencyMs,
			TokenCount:      tokenCount,
			ReasoningTokens: reasoningTokens,
			Detail:          truncateDetail("empty response content"),
		}
	}

	matchContent := contentTrimmed
	if runes := []rune(matchContent); len(runes) > probeMatchWindowRunes {
		matchContent = string(runes[:probeMatchWindowRunes])
	}
	evalContent := matchContent
	if !probe.CaseSensitive {
		evalContent = strings.ToLower(evalContent)
	}

	for _, exclude := range probe.ExcludeWords {
		ex := strings.TrimSpace(exclude)
		if ex == "" {
			continue
		}
		if !probe.CaseSensitive {
			ex = strings.ToLower(ex)
		}
		if strings.Contains(evalContent, ex) {
			return &DegradationProbeResult{
				Status:          model.DegradationStatusMismatched,
				LatencyMs:       latencyMs,
				TokenCount:      tokenCount,
				ReasoningTokens: reasoningTokens,
				Detail:          truncateDetail(fmt.Sprintf("hit exclude word [%s]: %s", exclude, contentTrimmed)),
			}
		}
	}

	matched := false
	switch probe.MatchRule {
	case "all":
		allHit := true
		compared := 0
		for _, kw := range probe.Keywords {
			target := strings.TrimSpace(kw)
			if target == "" {
				continue
			}
			compared++
			if !probe.CaseSensitive {
				target = strings.ToLower(target)
			}
			if !strings.Contains(evalContent, target) {
				allHit = false
				break
			}
		}
		// compared==0（关键词全为空白）时视为未命中，而不是无条件通过；
		// 正常配置已在保存校验时拦截，这里保持匹配器语义自洽。
		matched = compared > 0 && allHit
	case "regex":
		var compileErr error
		for _, pattern := range probe.Keywords {
			if strings.TrimSpace(pattern) == "" {
				continue
			}
			expr := pattern
			if !probe.CaseSensitive {
				expr = "(?i)" + expr
			}
			re, err := regexp.Compile(expr)
			if err != nil {
				compileErr = err
				continue
			}
			if re.MatchString(matchContent) {
				matched = true
				break
			}
		}
		if !matched && compileErr != nil {
			return &DegradationProbeResult{
				Status:          model.DegradationStatusError,
				LatencyMs:       latencyMs,
				TokenCount:      tokenCount,
				ReasoningTokens: reasoningTokens,
				Detail:          truncateDetail(fmt.Sprintf("invalid regex keyword: %v", compileErr)),
			}
		}
	default: // "any"
		for _, kw := range probe.Keywords {
			target := strings.TrimSpace(kw)
			if target == "" {
				continue
			}
			if !probe.CaseSensitive {
				target = strings.ToLower(target)
			}
			if strings.Contains(evalContent, target) {
				matched = true
				break
			}
		}
	}

	if !matched {
		return &DegradationProbeResult{
			Status:          model.DegradationStatusMismatched,
			LatencyMs:       latencyMs,
			TokenCount:      tokenCount,
			ReasoningTokens: reasoningTokens,
			Detail:          truncateDetail(contentTrimmed),
		}
	}

	return &DegradationProbeResult{
		Status:          model.DegradationStatusPassed,
		LatencyMs:       latencyMs,
		TokenCount:      tokenCount,
		ReasoningTokens: reasoningTokens,
		Detail:          "",
	}
}

func truncateDetail(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= 500 {
		return s
	}
	return string(runes[:500]) + "..."
}

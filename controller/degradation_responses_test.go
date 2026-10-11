package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/degradation_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestDegradationProbeResponsesPolicy(t *testing.T) {
	for _, tc := range []struct {
		name               string
		pattern            string
		mapping            string
		disabled           bool
		globalPassThrough  bool
		channelPassThrough bool
		useResponses       bool
	}{
		{name: "matching policy", pattern: "^gpt-4o-mini$", useResponses: true},
		{name: "policy matches original model before mapping", pattern: "^gpt-4o-mini$", mapping: `{"gpt-4o-mini":"gpt-4o"}`, useResponses: true},
		{name: "policy does not match mapped model alone", pattern: "^gpt-4o$", mapping: `{"gpt-4o-mini":"gpt-4o"}`},
		{name: "disabled policy", pattern: "^gpt-4o-mini$", disabled: true},
		{name: "global passthrough skips policy", pattern: "^gpt-4o-mini$", globalPassThrough: true},
		{name: "channel passthrough skips policy", pattern: "^gpt-4o-mini$", channelPassThrough: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := setupDegradationTaskTest(t)
			channel.ModelMapping = lo.ToPtr(tc.mapping)
			if tc.channelPassThrough {
				channel.Setting = lo.ToPtr(`{"pass_through_body_enabled":true}`)
			}
			settings := model_setting.GetGlobalSettings()
			previous := *settings
			t.Cleanup(func() { *settings = previous })
			settings.PassThroughRequestEnabled = tc.globalPassThrough
			settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
				Enabled: !tc.disabled, ChannelIDs: []int{channel.Id}, ModelPatterns: []string{tc.pattern},
			}
			expectedModel := "gpt-4o-mini"
			if tc.mapping != "" {
				expectedModel = "gpt-4o"
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/v1/chat/completions"
				if tc.useResponses {
					expectedPath = "/v1/responses"
				}
				if !assert.Equal(t, expectedPath, r.URL.Path) {
					w.WriteHeader(http.StatusNotFound)
					_, _ = io.WriteString(w, `{"error":{"message":"unexpected probe endpoint"}}`)
					return
				}
				body, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					return
				}
				assert.Equal(t, expectedModel, gjson.GetBytes(body, "model").String())
				w.Header().Set("Content-Type", "application/json")
				if tc.useResponses {
					assert.Equal(t, "Reply with ok", gjson.GetBytes(body, "input.0.content").String())
					_, _ = io.WriteString(w, `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":10,"output_tokens":1,"total_tokens":11}}`)
					return
				}
				_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`)
			}))
			t.Cleanup(upstream.Close)
			channel.BaseURL = lo.ToPtr(upstream.URL)
			service.InitHttpClient()
			userID, err := resolveChannelTestUserID(nil)
			require.NoError(t, err)

			result := ExecuteDegradationProbe(context.Background(), userID, channel, "gpt-4o-mini", degradation_setting.ProbeConfig{
				ID: "p1", Name: "Probe", Prompt: "Reply with ok", Keywords: []string{"ok"}, MaxTokens: 16,
			}, nil)

			assert.Equal(t, model.DegradationStatusPassed, result.Status, result.Detail)
			assert.Equal(t, 1, result.TokenCount)
		})
	}
}

type degradationProbeRoundTripper func(*http.Request) (*http.Response, error)

func TestDegradationProbeResponsesFailureIsExecutionError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		detail string
	}{
		{name: "failed with matching output", body: `{"status":"failed","error":{"code":"server_error","message":"upstream generation failed"},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`, detail: "upstream generation failed"},
		{name: "failed with empty output", body: `{"status":"failed","error":{"code":"server_error","message":"upstream generation failed"},"output":[]}`, detail: "upstream generation failed"},
		{name: "failed without error details", body: `{"status":"failed","error":null,"output":[]}`, detail: "responses request failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := setupDegradationTaskTest(t)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/responses", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(upstream.Close)
			channel.BaseURL = lo.ToPtr(upstream.URL)
			service.InitHttpClient()
			userID, err := resolveChannelTestUserID(nil)
			require.NoError(t, err)

			result := ExecuteDegradationProbe(context.Background(), userID, channel, "gpt-5-codex", degradation_setting.ProbeConfig{
				ID: "p1", Name: "Probe", Prompt: "Reply with ok", Keywords: []string{"ok"}, MaxTokens: 16,
			}, nil)

			assert.Equal(t, model.DegradationStatusError, result.Status)
			assert.Contains(t, result.Detail, tc.detail)
		})
	}
}

func (f degradationProbeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestDegradationProbeBufferedStreamWithKeepAlive(t *testing.T) {
	channel := setupDegradationTaskTest(t)
	channel.Type = constant.ChannelTypeCodex
	channel.Key = `{"access_token":"test-access-token","account_id":"test-account"}`
	channel.BaseURL = lo.ToPtr("https://probe.example")
	settings := operation_setting.GetGeneralSetting()
	previousEnabled, previousSeconds := settings.PingIntervalEnabled, settings.PingIntervalSeconds
	settings.PingIntervalEnabled, settings.PingIntervalSeconds = true, 1
	t.Cleanup(func() {
		settings.PingIntervalEnabled, settings.PingIntervalSeconds = previousEnabled, previousSeconds
	})
	service.InitHttpClient()
	client := service.GetHttpClient()
	previousTransport := client.Transport
	t.Cleanup(func() { client.Transport = previousTransport })
	userID, err := resolveChannelTestUserID(nil)
	require.NoError(t, err)

	synctest.Test(t, func(t *testing.T) {
		client.Transport = degradationProbeRoundTripper(func(r *http.Request) (*http.Response, error) {
			assert.Equal(t, "/backend-api/codex/responses", r.URL.Path)
			// Virtual time reproduces a slow response header without a wall-clock sleep.
			synctest.Wait()
			time.Sleep(2 * time.Second)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":10,\"output_tokens\":1,\"total_tokens\":11}}}\n\n")),
			}, nil
		})
		result := ExecuteDegradationProbe(context.Background(), userID, channel, "gpt-5-codex", degradation_setting.ProbeConfig{
			ID: "p1", Name: "Probe", Prompt: "Reply with ok", Keywords: []string{"ok"}, MaxTokens: 16,
		}, nil)

		assert.Equal(t, model.DegradationStatusPassed, result.Status, result.Detail)
		assert.Equal(t, 1, result.TokenCount)
	})
}

func TestDegradationProbeResponsesTargets(t *testing.T) {
	for _, tc := range []struct {
		name        string
		channelType int
		model       string
		mapping     string
		stream      bool
		inherit     bool
	}{
		{name: "Codex channel uses streamed Responses", channelType: constant.ChannelTypeCodex, model: "gpt-5", stream: true},
		{name: "Responses-only model on OpenAI channel", channelType: constant.ChannelTypeOpenAI, model: "gpt-5-codex"},
		{name: "mapped Responses-only model", channelType: constant.ChannelTypeOpenAI, model: "probe-alias", mapping: `{"probe-alias":"gpt-5-codex"}`},
		{name: "Responses-only model inherits reasoning without unsupported sampling", channelType: constant.ChannelTypeOpenAI, model: "gpt-5-codex", inherit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := setupDegradationTaskTest(t)
			channel.Type = tc.channelType
			channel.ModelMapping = lo.ToPtr(tc.mapping)
			if tc.channelType == constant.ChannelTypeCodex {
				channel.Key = `{"access_token":"test-access-token","account_id":"test-account"}`
			}
			expectedModel := tc.model
			if tc.mapping != "" {
				expectedModel = "gpt-5-codex"
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/v1/responses"
				if tc.channelType == constant.ChannelTypeCodex {
					expectedPath = "/backend-api/codex/responses"
					assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
				}
				assert.Equal(t, expectedPath, r.URL.Path)
				body, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					return
				}
				assert.Equal(t, expectedModel, gjson.GetBytes(body, "model").String())
				assert.Equal(t, "Reply with ok", gjson.GetBytes(body, "input.0.content").String())
				assert.Equal(t, "Be precise", gjson.GetBytes(body, "instructions").String())
				if tc.inherit {
					assert.False(t, gjson.GetBytes(body, "reasoning").Exists())
				} else {
					assert.Equal(t, "high", gjson.GetBytes(body, "reasoning.effort").String())
				}
				assert.Equal(t, tc.stream, gjson.GetBytes(body, "stream").Bool())
				assert.False(t, gjson.GetBytes(body, "temperature").Exists())
				assert.False(t, gjson.GetBytes(body, "max_tokens").Exists())
				if tc.channelType == constant.ChannelTypeCodex {
					assert.False(t, gjson.GetBytes(body, "max_output_tokens").Exists())
				} else if tc.inherit {
					assert.Equal(t, int64(256), gjson.GetBytes(body, "max_output_tokens").Int())
				} else {
					assert.Equal(t, int64(4352), gjson.GetBytes(body, "max_output_tokens").Int())
				}
				if tc.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":7,\"total_tokens\":17,\"output_tokens_details\":{\"reasoning_tokens\":5}}}}\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":10,"output_tokens":7,"total_tokens":17,"output_tokens_details":{"reasoning_tokens":5}}}`)
			}))
			t.Cleanup(upstream.Close)
			channel.BaseURL = lo.ToPtr(upstream.URL)
			service.InitHttpClient()
			userID, err := resolveChannelTestUserID(nil)
			require.NoError(t, err)

			binding := &degradation_setting.ModelProbeBinding{ReasoningEffort: "high"}
			if tc.inherit {
				binding = nil
			}
			result := ExecuteDegradationProbe(context.Background(), userID, channel, tc.model, degradation_setting.ProbeConfig{
				ID: "p1", Name: "Probe", Prompt: "Reply with ok", SystemPrompt: "Be precise", Keywords: []string{"ok"}, MaxTokens: 256,
			}, binding)

			assert.Equal(t, model.DegradationStatusPassed, result.Status, result.Detail)
			assert.Equal(t, 7, result.TokenCount)
			assert.Equal(t, 5, result.ReasoningTokens)
		})
	}
}

func TestDegradationProbeResponsesStreamFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event string
	}{
		{name: "failed response", event: `{"type":"response.failed","response":{"error":{"type":"server_error","message":"upstream probe failed"}}}`},
		{name: "top-level error after matching text", event: `{"type":"error","code":"server_error","message":"upstream probe failed","param":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := setupDegradationTaskTest(t)
			channel.Type = constant.ChannelTypeCodex
			channel.Key = `{"access_token":"test-access-token","account_id":"test-account"}`
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: "+tc.event+"\n\n")
			}))
			t.Cleanup(upstream.Close)
			channel.BaseURL = lo.ToPtr(upstream.URL)
			service.InitHttpClient()
			userID, err := resolveChannelTestUserID(nil)
			require.NoError(t, err)

			result := ExecuteDegradationProbe(context.Background(), userID, channel, "gpt-5-codex", degradation_setting.ProbeConfig{
				ID: "p1", Name: "Probe", Prompt: "Reply with ok", Keywords: []string{"ok"}, MaxTokens: 256,
			}, nil)

			assert.Equal(t, model.DegradationStatusError, result.Status)
			assert.Contains(t, result.Detail, "upstream probe failed")
		})
	}
}

func TestDegradationProbeResponsesStreamRequiresTerminalResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		event  string
		status int
	}{
		{name: "EOF after matching text", status: model.DegradationStatusError},
		{name: "DONE sentinel after matching text", event: `[DONE]`, status: model.DegradationStatusError},
		{name: "completed event without response", event: `{"type":"response.completed"}`, status: model.DegradationStatusError},
		{name: "completed response", event: `{"type":"response.completed","response":{"status":"completed"}}`, status: model.DegradationStatusPassed},
		{name: "done response", event: `{"type":"response.done","response":{"status":"completed"}}`, status: model.DegradationStatusPassed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := setupDegradationTaskTest(t)
			channel.Type = constant.ChannelTypeCodex
			channel.Key = `{"access_token":"test-access-token","account_id":"test-account"}`
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")
				if tc.event != "" {
					_, _ = io.WriteString(w, "data: "+tc.event+"\n\n")
				}
			}))
			t.Cleanup(upstream.Close)
			channel.BaseURL = lo.ToPtr(upstream.URL)
			service.InitHttpClient()
			userID, err := resolveChannelTestUserID(nil)
			require.NoError(t, err)

			result := ExecuteDegradationProbe(context.Background(), userID, channel, "gpt-5-codex", degradation_setting.ProbeConfig{
				ID: "p1", Name: "Probe", Prompt: "Reply with ok", Keywords: []string{"ok"}, MaxTokens: 256,
			}, nil)

			assert.Equal(t, tc.status, result.Status, result.Detail)
			if tc.status == model.DegradationStatusError {
				assert.Contains(t, result.Detail, "responses stream ended without a terminal response")
			}
		})
	}
}

func TestDegradationProbeResponsesStreamCancellation(t *testing.T) {
	channel := setupDegradationTaskTest(t)
	channel.Type = constant.ChannelTypeCodex
	channel.Key = `{"access_token":"test-access-token","account_id":"test-account"}`
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")
		w.(http.Flusher).Flush()
		cancel()
		<-r.Context().Done()
	}))
	t.Cleanup(upstream.Close)
	channel.BaseURL = lo.ToPtr(upstream.URL)
	service.InitHttpClient()
	userID, err := resolveChannelTestUserID(nil)
	require.NoError(t, err)

	result := ExecuteDegradationProbe(ctx, userID, channel, "gpt-5-codex", degradation_setting.ProbeConfig{
		ID: "p1", Name: "Probe", Prompt: "Reply with ok", Keywords: []string{"ok"}, MaxTokens: 256,
	}, nil)

	require.ErrorIs(t, ctx.Err(), context.Canceled)
	assert.Equal(t, model.DegradationStatusError, result.Status)
}

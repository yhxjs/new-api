package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPassThroughModelMappingReachesUpstream(t *testing.T) {
	originalGlobal := model_setting.GetGlobalSettings().PassThroughRequestEnabled
	t.Cleanup(func() { model_setting.GetGlobalSettings().PassThroughRequestEnabled = originalGlobal })

	tests := []struct {
		name        string
		path        string
		fields      string
		channelType int
		request     dto.Request
		handler     func(*gin.Context, *relaycommon.RelayInfo) *types.NewAPIError
		contentType string
	}{
		{"chat", "/v1/chat/completions", `"messages":[{"role":"user","content":"hello"}],"temperature":0`, constant.ChannelTypeOpenAI, &dto.GeneralOpenAIRequest{}, TextHelper, "application/json"},
		{"responses", "/v1/responses", `"input":"hello","store":false`, constant.ChannelTypeOpenAI, &dto.OpenAIResponsesRequest{}, ResponsesHelper, "application/json"},
		{"compact", "/v1/responses/compact", `"input":"hello"`, constant.ChannelTypeOpenAI, &dto.OpenAIResponsesCompactionRequest{}, ResponsesHelper, "application/json"},
		{"claude", "/v1/messages", `"messages":[{"role":"user","content":"hello"}],"max_tokens":16`, constant.ChannelTypeAnthropic, &dto.ClaudeRequest{}, ClaudeHelper, "application/json"},
		{"image", "/v1/images/generations", `"prompt":"hello","n":1`, constant.ChannelTypeOpenAI, &dto.ImageRequest{}, ImageHelper, "application/json"},
		{"rerank", "/v1/rerank", `"query":"hello","documents":["document"]`, constant.ChannelTypeJina, &dto.RerankRequest{}, RerankHelper, "application/json"},
		{"gemini", "/v1beta/models/alias:generateContent", `"contents":[{"parts":[{"text":"hello"}]}]`, constant.ChannelTypeGemini, &dto.GeminiChatRequest{}, GeminiHelper, "application/json"},
		{"image form", "/v1/images/edits", "model=alias&prompt=hello+world&vendor=%2f", constant.ChannelTypeOpenAI, &dto.ImageRequest{}, ImageHelper, "application/x-www-form-urlencoded"},
		{"image multipart", "/v1/images/edits", "--upload\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nalias\r\n--upload\r\nContent-Disposition: form-data; name=\"image\"; filename=\"image.png\"\r\n\r\n\x00alias\xff\r\n--upload--\r\n", constant.ChannelTypeOpenAI, &dto.ImageRequest{}, ImageHelper, "multipart/form-data; boundary=upload"},
	}
	for _, testCase := range tests {
		for _, global := range []bool{false, true} {
			mode := "channel"
			if global {
				mode = "global"
			}
			t.Run(testCase.name+"/"+mode, func(t *testing.T) {
				model_setting.GetGlobalSettings().PassThroughRequestEnabled = global
				input := "{\n  \"model\":\"alias\",\n  \"vendor\":{\"model\":\"nested\",\"count\":9007199254740993},\n  " + testCase.fields + "\n}\n"
				if testCase.channelType == constant.ChannelTypeGemini {
					input = strings.Replace(input, "  \"model\":\"alias\",\n", "", 1)
				}
				if testCase.contentType != "application/json" {
					input = testCase.fields
				}
				type capturedRequest struct {
					body        []byte
					path        string
					length      int64
					contentType string
					err         error
				}
				requests := make(chan capturedRequest, 1)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					requests <- capturedRequest{body, r.URL.Path, r.ContentLength, r.Header.Get("Content-Type"), err}
					// Stop after forwarding so the test does not enter billing settlement.
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":{"message":"request captured","type":"invalid_request_error"}}`)
				}))
				t.Cleanup(upstream.Close)

				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, testCase.path, strings.NewReader(input))
				c.Request.Header.Set("Content-Type", testCase.contentType)
				t.Cleanup(func() { common.CleanupBodyStorage(c) })
				require.NoError(t, common.UnmarshalBodyReusable(c, testCase.request))
				common.SetContextKey(c, constant.ContextKeyOriginalModel, "alias")
				common.SetContextKey(c, constant.ContextKeyChannelType, testCase.channelType)
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{
					PassThroughBodyEnabled: !global,
					SystemPrompt:           "must not be injected",
				})
				common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]any{"model": "must-not-override"})
				info := &relaycommon.RelayInfo{
					OriginModelName: "alias",
					RequestURLPath:  testCase.path,
					RelayMode:       relayconstant.Path2RelayMode(testCase.path),
					Request:         testCase.request,
				}

				mappings := []struct {
					config string
					model  string
				}{
					{`{"alias":"middle","middle":"target"}`, "target"},
					{`{"alias":"another-target"}`, "another-target"},
					{`{}`, "alias"},
				}
				if testCase.name == "claude" {
					mappings[0].config = `{"alias":"claude-opus-4-6-high"}`
					mappings[0].model = "claude-opus-4-6-high"
				}
				for _, mapping := range mappings {
					common.SetContextKey(c, constant.ContextKeyChannelModelMapping, mapping.config)
					apiErr := testCase.handler(c, info)
					require.NotNil(t, apiErr)
					require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
					select {
					case got := <-requests:
						require.NoError(t, got.err)
						expected := strings.Replace(input, "\"model\":\"alias\"", "\"model\":\""+mapping.model+"\"", 1)
						if testCase.contentType == "application/x-www-form-urlencoded" {
							expected = strings.Replace(input, "model=alias", "model="+mapping.model, 1)
						} else if strings.HasPrefix(testCase.contentType, "multipart/form-data") {
							expected = strings.Replace(input, "\r\n\r\nalias\r\n", "\r\n\r\n"+mapping.model+"\r\n", 1)
						}
						assert.Equal(t, expected, string(got.body))
						assert.EqualValues(t, len(expected), got.length)
						assert.Equal(t, testCase.contentType, got.contentType)
						if testCase.channelType == constant.ChannelTypeGemini {
							assert.Contains(t, got.path, "/models/"+mapping.model+":generateContent")
						}
					default:
						require.FailNow(t, "request did not reach upstream")
					}
					assert.Equal(t, "alias", info.OriginModelName)
					storage, err := common.GetBodyStorage(c)
					require.NoError(t, err)
					original, err := storage.Bytes()
					require.NoError(t, err)
					assert.Equal(t, input, string(original))
				}
			})
		}
	}
}

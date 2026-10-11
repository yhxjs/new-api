package aws

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestDegradationProbeAllowsAWSSDKRequestToReturnNilHTTPResponse(t *testing.T) {
	service.InitHttpClient()
	client := service.GetHttpClient()
	require.NotNil(t, client)
	originalTransport := client.Transport
	t.Cleanup(func() { client.Transport = originalTransport })
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude-sonnet-4","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)),
			Request:    req,
		}, nil
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access|secret|us-east-1",
			UpstreamModelName: "claude-sonnet-4",
		},
		OriginModelName: "claude-sonnet-4",
		IsStream:        false,
		RelayFormat:     types.RelayFormatOpenAI,
	}
	adaptor := &Adaptor{}

	raw, err := doAwsClientRequest(c, info, adaptor, strings.NewReader(`{"model":"claude-sonnet-4","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	require.Nil(t, raw, "AWS SDK request preparation returns no HTTP response; DoResponse performs InvokeModel")

	_, responseErr := adaptor.DoResponse(c, nil, info)
	require.Nil(t, responseErr)
	require.Contains(t, recorder.Body.String(), "ok")
}

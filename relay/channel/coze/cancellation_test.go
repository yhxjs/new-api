package coze

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cancelAfterPollTransport struct {
	http.RoundTripper
	cancel context.CancelFunc
}

func (transport cancelAfterPollTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.RoundTripper.RoundTrip(request)
	if err == nil && request.URL.Path == "/v3/chat/retrieve" {
		response.Body = cancelAfterPollBody{ReadCloser: response.Body, cancel: transport.cancel}
	}
	return response, err
}

type cancelAfterPollBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (body cancelAfterPollBody) Close() error {
	err := body.ReadCloser.Close()
	body.cancel()
	return err
}

func TestCozeCancellationBetweenPolls(t *testing.T) {
	for _, isChannelTest := range []bool{true, false} {
		name := "probe_cancels"
		if !isChannelTest {
			name = "ordinary_request_finishes"
		}
		t.Run(name, func(t *testing.T) {
			requestContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			pollConsumed := make(chan struct{})
			cancelAfterPoll := sync.OnceFunc(func() {
				cancel()
				close(pollConsumed)
			})
			var polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				switch request.URL.Path {
				case "/v3/chat":
					_, _ = io.WriteString(writer, `{"code":0,"data":{"id":"chat","conversation_id":"conversation"}}`)
				case "/v3/chat/retrieve":
					if polls.Add(1) == 1 {
						_, _ = io.WriteString(writer, `{"code":0,"data":{"status":"in_progress"}}`)
						return
					}
					_, _ = io.WriteString(writer, `{"code":0,"data":{"status":"completed"}}`)
				case "/v3/chat/message/list":
					_, _ = io.WriteString(writer, `{"code":0,"data":[]}`)
				}
			}))
			defer server.Close()
			service.InitHttpClient()
			client := service.GetHttpClient()
			originalTransport := client.Transport
			client.Transport = cancelAfterPollTransport{RoundTripper: originalTransport, cancel: cancelAfterPoll}
			defer func() { client.Transport = originalTransport }()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}")).WithContext(requestContext)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL}, IsChannelTest: isChannelTest}

			// Cancel exactly after the first in-progress poll has been consumed and
			// closed, so the next operation is the polling wait rather than HTTP I/O.
			result := make(chan error, 1)
			workerDone := make(chan struct{})
			go func() {
				defer close(workerDone)
				adaptor := &Adaptor{}
				response, err := adaptor.DoRequest(c, info, strings.NewReader("{}"))
				if err != nil {
					result <- err
					return
				}
				httpResponse := response.(*http.Response)
				defer httpResponse.Body.Close()
				_, apiErr := adaptor.DoResponse(c, httpResponse, info)
				if apiErr != nil {
					result <- apiErr
					return
				}
				result <- nil
			}()
			defer func() {
				select {
				case <-workerDone:
				case <-time.After(3 * time.Second):
					t.Error("polling worker did not exit during cleanup")
				}
			}()
			select {
			case <-pollConsumed:
			case <-time.After(3 * time.Second):
				require.FailNow(t, "first poll was not consumed")
			}
			returnDeadline := 250 * time.Millisecond
			if !isChannelTest {
				returnDeadline = 3 * time.Second
			}
			select {
			case err := <-result:
				if isChannelTest {
					assert.ErrorIs(t, err, context.Canceled)
					assert.Equal(t, int32(1), polls.Load(), "canceled polling must not issue another upstream request")
				} else {
					assert.NoError(t, err, "ordinary requests must finish after client cancellation")
					assert.Equal(t, int32(2), polls.Load())
				}
			case <-time.After(returnDeadline):
				require.FailNow(t, "polling did not return after the first poll canceled the client context")
			}
		})
	}
}

func TestCozeCancellationInterruptsUpstreamHTTP(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		body bool
	}{
		{name: "retrieve_headers", path: "/v3/chat/retrieve"},
		{name: "retrieve_body", path: "/v3/chat/retrieve", body: true},
		{name: "detail_headers", path: "/v3/chat/message/list"},
		{name: "detail_body", path: "/v3/chat/message/list", body: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requestContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				if request.URL.Path == test.path {
					if test.body {
						writer.WriteHeader(http.StatusOK)
						writer.(http.Flusher).Flush()
					}
					close(entered)
					select {
					case <-request.Context().Done():
						return
					case <-release:
					}
				}
				switch request.URL.Path {
				case "/v3/chat":
					_, _ = io.WriteString(writer, `{"code":0,"data":{"id":"chat","conversation_id":"conversation"}}`)
				case "/v3/chat/retrieve":
					_, _ = io.WriteString(writer, `{"code":0,"data":{"status":"completed"}}`)
				case "/v3/chat/message/list":
					_, _ = io.WriteString(writer, `{"code":0,"data":[]}`)
				}
			}))
			defer server.Close()
			defer close(release)
			service.InitHttpClient()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}")).WithContext(requestContext)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL}, IsChannelTest: true}
			result := make(chan error, 1)
			go func() {
				adaptor := &Adaptor{}
				response, err := adaptor.DoRequest(c, info, strings.NewReader("{}"))
				if err != nil {
					result <- err
					return
				}
				httpResponse := response.(*http.Response)
				defer httpResponse.Body.Close()
				_, apiErr := adaptor.DoResponse(c, httpResponse, info)
				if apiErr != nil {
					result <- apiErr
					return
				}
				result <- nil
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				require.FailNow(t, "upstream request was not reached")
			}
			cancel()
			select {
			case err := <-result:
				assert.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				require.FailNow(t, "cancellation did not unblock the upstream HTTP operation")
			}
		})
	}
}

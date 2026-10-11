package xunfei

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXunfeiHandlerRejectsCanceledRequest(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestContext)

	usage, err := xunfeiHandler(c, &relaycommon.RelayInfo{IsChannelTest: true}, dto.GeneralOpenAIRequest{Model: "SparkDesk-v1.1"}, "app", "secret", "key")
	assert.Nil(t, usage)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestXunfeiOrdinaryRequestFinishesAfterClientCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		if _, _, err := connection.ReadMessage(); err != nil {
			return
		}
		_ = connection.WriteMessage(websocket.TextMessage, []byte(`{"payload":{"choices":{"status":2,"text":[{"content":"complete"}]}}}`))
	}))
	defer server.Close()
	clientContext, cancelClient := context.WithCancel(context.Background())
	cancelClient()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(clientContext)
	requestContext, cancelRequest := newXunfeiRequestContext(c, &relaycommon.RelayInfo{})
	defer cancelRequest()

	responses, stopped, err := xunfeiMakeRequest(requestContext, dto.GeneralOpenAIRequest{}, "lite", "ws"+strings.TrimPrefix(server.URL, "http"), "app")
	require.NoError(t, err, "ordinary requests must continue after client cancellation")
	select {
	case response := <-responses:
		require.Len(t, response.Payload.Choices.Text, 1)
		assert.Equal(t, "complete", response.Payload.Choices.Text[0].Content)
	case <-time.After(time.Second):
		require.FailNow(t, "ordinary request did not receive the upstream response")
	}
	select {
	case stop := <-stopped:
		assert.True(t, stop)
	case <-time.After(time.Second):
		require.FailNow(t, "ordinary request did not finish")
	}
}

func TestXunfeiCancellationInterruptsHandshake(t *testing.T) {
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(entered)
		select {
		case <-request.Context().Done():
		case <-release:
		}
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	defer close(release)
	result := make(chan error, 1)
	go func() {
		_, _, err := xunfeiMakeRequest(requestContext, dto.GeneralOpenAIRequest{}, "lite", "ws"+strings.TrimPrefix(server.URL, "http"), "app")
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "upstream handshake was not reached")
	}
	cancel()
	select {
	case err := <-result:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		require.FailNow(t, "cancellation did not unblock the WebSocket handshake")
	}
}

func TestXunfeiCancellationReleasesWebsocket(t *testing.T) {
	for _, sendResponse := range []bool{false, true} {
		name := "blocked_read"
		if sendResponse {
			name = "without_response_consumer"
		}
		t.Run(name, func(t *testing.T) {
			requestContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			connections := make(chan *websocket.Conn, 1)
			ready := make(chan error, 1)
			closed := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
				if err != nil {
					ready <- err
					return
				}
				defer connection.Close()
				connections <- connection
				_, _, err = connection.ReadMessage()
				if err == nil && sendResponse {
					err = connection.WriteMessage(websocket.TextMessage, []byte(`{"payload":{"choices":{"status":0,"text":[{"content":"partial"}]}}}`))
				}
				ready <- err
				_, _, err = connection.ReadMessage()
				closed <- err
			}))
			defer server.Close()
			_, stopped, err := xunfeiMakeRequest(requestContext, dto.GeneralOpenAIRequest{}, "lite", "ws"+strings.TrimPrefix(server.URL, "http"), "app")
			require.NoError(t, err)
			connection := <-connections
			defer connection.Close()
			select {
			case err := <-ready:
				require.NoError(t, err)
			case <-time.After(3 * time.Second):
				require.FailNow(t, "upstream did not receive the request")
			}
			cancel()
			select {
			case stopped := <-stopped:
				assert.True(t, stopped)
			case <-time.After(time.Second):
				require.FailNow(t, "canceled WebSocket reader did not exit")
			}
			select {
			case err := <-closed:
				assert.Error(t, err, "the upstream connection must be closed")
			case <-time.After(time.Second):
				require.FailNow(t, "cancellation did not release the upstream connection")
			}
		})
	}
}

func TestXunfeiCompletionClosesConnectionWithoutStopConsumer(t *testing.T) {
	connections := make(chan *websocket.Conn, 1)
	closed := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			closed <- err
			return
		}
		defer connection.Close()
		connections <- connection
		_, _, err = connection.ReadMessage()
		if err != nil {
			closed <- err
			return
		}
		response := XunfeiChatResponse{}
		response.Payload.Choices.Status = 2
		response.Payload.Choices.Text = []XunfeiChatResponseTextItem{{Content: "complete"}}
		payload, err := common.Marshal(response)
		if err == nil {
			err = connection.WriteMessage(websocket.TextMessage, payload)
		}
		if err != nil {
			closed <- err
			return
		}
		_, _, err = connection.ReadMessage()
		closed <- err
	}))
	defer server.Close()
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	responses, stopped, err := xunfeiMakeRequest(requestContext, dto.GeneralOpenAIRequest{}, "lite", "ws"+strings.TrimPrefix(server.URL, "http"), "app")
	require.NoError(t, err)
	connection := <-connections
	defer connection.Close()
	select {
	case response := <-responses:
		assert.Equal(t, 2, response.Payload.Choices.Status)
		assert.Equal(t, "complete", response.Payload.Choices.Text[0].Content)
	case <-time.After(time.Second):
		require.FailNow(t, "terminal upstream response was not received")
	}
	select {
	case err := <-closed:
		assert.Error(t, err, "completion must close the upstream connection before the stop signal is consumed")
	case <-time.After(time.Second):
		require.FailNow(t, "reader leaked the connection while waiting to deliver the stop signal")
	}
	select {
	case stop := <-stopped:
		assert.True(t, stop)
	case <-time.After(time.Second):
		require.FailNow(t, "completion signal was not delivered")
	}
}

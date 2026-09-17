package common

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	hostcommon "github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPassThroughRequestBodyRewritesOnlyTopLevelModel(t *testing.T) {
	input := `{"model":"alias","messages":[{"role":"user","content":"hello","metadata":{"model":"nested"}}],"temperature":0}`
	expected := `{"model":"target","messages":[{"role":"user","content":"hello","metadata":{"model":"nested"}}],"temperature":0}`

	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(input))
	ginContext.Request.Header.Set("Content-Type", "application/json; charset=utf-8")
	originalStorage, err := hostcommon.CreateBodyStorage([]byte(input))
	require.NoError(t, err)
	ginContext.Set(hostcommon.KeyBodyStorage, originalStorage)
	t.Cleanup(func() { require.NoError(t, originalStorage.Close()) })

	requestBody, closer, err := NewPassThroughRequestBody(ginContext, "target", true)
	require.NoError(t, err)
	require.NotNil(t, closer)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	got, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	assert.Equal(t, []byte(expected), got)

	original, err := originalStorage.Bytes()
	require.NoError(t, err)
	assert.Equal(t, []byte(input), original)
}

func TestNewPassThroughRequestBodyKeepsOriginalWhenModelIsNotMapped(t *testing.T) {
	input := `{"model":"alias","temperature":0}`

	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(input))
	ginContext.Request.Header.Set("Content-Type", "application/json")
	storage, err := hostcommon.CreateBodyStorage([]byte(input))
	require.NoError(t, err)
	ginContext.Set(hostcommon.KeyBodyStorage, storage)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })

	requestBody, closer, err := NewPassThroughRequestBody(ginContext, "alias", false)
	require.NoError(t, err)
	assert.Nil(t, closer)

	got, err := io.ReadAll(requestBody)
	require.NoError(t, err)
	assert.Equal(t, []byte(input), got)
}

func TestNewPassThroughRequestBodyReplaysMappedDiskBody(t *testing.T) {
	previousConfig := hostcommon.GetDiskCacheConfig()
	hostcommon.SetDiskCacheConfig(hostcommon.DiskCacheConfig{
		Enabled: true, ThresholdMB: 0, MaxSizeMB: 64, Path: t.TempDir(),
	})
	t.Cleanup(func() { hostcommon.SetDiskCacheConfig(previousConfig) })

	input := " {\n\t\"mo\\u0064el\" : \"alias\",\"vendor\":{\"model\":\"alias\",\"n\":9007199254740993}}\n"
	want := strings.Replace(input, `"alias"`, `"target"`, 1)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(input))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { hostcommon.CleanupBodyStorage(c) })

	body, closer, err := NewPassThroughRequestBody(c, "target", true)
	require.NoError(t, err)
	require.NotNil(t, closer)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	upstreamStorage, ok := closer.(hostcommon.BodyStorage)
	require.True(t, ok)
	assert.True(t, upstreamStorage.IsDisk())
	assertIndependentReplayReaders(t, []byte(want), body)
	assert.EqualValues(t, len(want), body.Size())

	require.NoError(t, closer.Close())
	_, err = body.NewReader()
	assert.ErrorIs(t, err, hostcommon.ErrStorageClosed)
	originalStorage, err := hostcommon.GetBodyStorage(c)
	require.NoError(t, err)
	original, err := originalStorage.Bytes()
	require.NoError(t, err)
	assert.Equal(t, input, string(original))
}

func TestNewPassThroughRequestBodyPreservesMultipartAndFormBytes(t *testing.T) {
	multipartBody := "preamble\r\n--upload\r\nContent-Disposition: form-data; name=\"image\"; filename=\"model.png\"\r\nContent-Type: image/png\r\n\r\n\x00alias\xff\r\n--upload-not-a-boundary\r\n\r\n" +
		"--upload\r\nContent-Disposition: form-data; name=\"model\"\r\nX-Custom:  preserve spacing\r\n\r\nalias\r\n" +
		"--upload\r\nContent-Disposition: form-data; name=\"prompt\"\r\n\r\nkeep alias and model unchanged\r\n--upload--\r\nepilogue"
	tests := []struct {
		name        string
		contentType string
		body        string
		want        string
	}{
		{"multipart", "multipart/form-data; boundary=upload", multipartBody, strings.Replace(multipartBody, "\r\n\r\nalias\r\n", "\r\n\r\ntarget/model\r\n", 1)},
		{"multipart LF", "multipart/form-data; boundary=upload", strings.ReplaceAll(multipartBody, "\r\n", "\n"), strings.Replace(strings.ReplaceAll(multipartBody, "\r\n", "\n"), "\n\nalias\n", "\n\ntarget/model\n", 1)},
		{"file named model", "multipart/form-data; boundary=upload", strings.Replace(multipartBody, `name="image"`, `name="model"`, 1), strings.Replace(strings.Replace(multipartBody, `name="image"`, `name="model"`, 1), "\r\n\r\nalias\r\n", "\r\n\r\ntarget/model\r\n", 1)},
		{"repeated model fields", "multipart/form-data; boundary=upload",
			"--upload\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nalias\r\n--upload\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nsecond\r\n--upload--",
			"--upload\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\ntarget/model\r\n--upload\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\ntarget/model\r\n--upload--"},
		{"boundary text in file", "multipart/form-data; boundary=upload",
			strings.Replace(multipartBody, "\x00alias\xff", "\x00alias\xff\n--upload\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nalias", 1),
			strings.Replace(strings.Replace(multipartBody, "\r\n\r\nalias\r\n", "\r\n\r\ntarget/model\r\n", 1), "\x00alias\xff", "\x00alias\xff\n--upload\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nalias", 1)},
		{"form", "application/x-www-form-urlencoded", "prompt=alias+%20&%6dodel=alias&extra=%2f&model=alias&n=1", "prompt=alias+%20&%6dodel=target%2Fmodel&extra=%2f&model=target%2Fmodel&n=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", tt.contentType)
			t.Cleanup(func() { hostcommon.CleanupBodyStorage(c) })

			body, closer, err := NewPassThroughRequestBody(c, "target/model", true)
			require.NoError(t, err)
			if closer != nil {
				t.Cleanup(func() { require.NoError(t, closer.Close()) })
			}
			got, err := io.ReadAll(body)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
			assert.Equal(t, tt.contentType, c.GetHeader("Content-Type"))
			storage, err := hostcommon.GetBodyStorage(c)
			require.NoError(t, err)
			original, err := storage.Bytes()
			require.NoError(t, err)
			assert.Equal(t, tt.body, string(original))
		})
	}
}

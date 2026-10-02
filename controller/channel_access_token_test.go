package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelRedactsBalanceQueryTokenCaseVariants(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		want     string
	}{
		{
			name:     "uppercase token",
			settings: `{"balance_query":{"mode":"user_api","ACCESS_TOKEN":"pat-upper","user_id":"1"}}`,
			want:     `{"balance_query":{"mode":"user_api","user_id":"1"}}`,
		},
		{
			name:     "uppercase balance query",
			settings: `{"BALANCE_QUERY":{"mode":"user_api","access_token":"pat-outer","user_id":"1"},"future":{"big":12345678901234567890}}`,
			want:     `{"BALANCE_QUERY":{"mode":"user_api","user_id":"1"},"future":{"big":12345678901234567890}}`,
		},
		{
			name:     "duplicate token aliases",
			settings: `{"balance_query":{"mode":"user_api","access_token":"pat-lower","Access_Token":"pat-mixed","user_id":"1"}}`,
			want:     `{"balance_query":{"mode":"user_api","user_id":"1"}}`,
		},
		{
			name:     "duplicate balance query aliases",
			settings: `{"balance_query":{"mode":"user_api","access_token":"pat-lower","user_id":"1"},"Balance_Query":{"mode":"user_api","ACCESS_TOKEN":"pat-other","user_id":"2"}}`,
			want:     `{"balance_query":{"mode":"user_api","user_id":"1"},"Balance_Query":{"mode":"user_api","user_id":"2"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupBalanceTestDB(t)
			channel := model.Channel{Type: constant.ChannelTypeNewAPI, OtherSettings: tt.settings}
			require.NoError(t, channel.ValidateSettings())
			require.NotEmpty(t, channel.GetOtherSettings().BalanceQuery.AccessToken)
			require.NoError(t, model.DB.Create(&channel).Error)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/"+strconv.Itoa(channel.Id), nil)
			ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channel.Id)}}

			GetChannel(ctx)

			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool          `json:"success"`
				Data    model.Channel `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)
			assert.JSONEq(t, tt.want, response.Data.OtherSettings)
			assert.NotContains(t, recorder.Body.String(), "pat-")
			var stored model.Channel
			require.NoError(t, model.DB.First(&stored, channel.Id).Error)
			assert.Equal(t, tt.settings, stored.OtherSettings)
		})
	}
}

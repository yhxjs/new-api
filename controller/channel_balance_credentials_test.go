package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomBalanceQueryCredentialsAreRedactedAndPreserved(t *testing.T) {
	for _, databaseType := range []common.DatabaseType{
		common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL,
	} {
		t.Run(string(databaseType), func(t *testing.T) {
			db := setupBalanceRegressionDB(t, databaseType)
			require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.Log{}, &model.User{}))
			previousLogDB, previousRedis := model.LOG_DB, common.RedisEnabled
			model.LOG_DB, common.RedisEnabled = db, false
			t.Cleanup(func() { model.LOG_DB, common.RedisEnabled = previousLogDB, previousRedis })
			baseURL := "https://upstream.example"
			settings := `{"balance_query":{"mode":"custom","method":"POST","url":"https://upstream.example/balance?token=pat-url-secret","headers":{"Authorization":"Bearer pat-header-secret","X-API-Key":"pat-extra-secret"},"body":"pat-body-secret","extract":"response.balance"},"future":{"quota":12345678901234567890}}`
			channel := model.Channel{Type: constant.ChannelTypeNewAPI, BaseURL: &baseURL, Key: "sk-test", OtherSettings: settings}
			require.NoError(t, channel.ValidateSettings())
			require.NoError(t, db.Create(&channel).Error)
			t.Cleanup(func() { require.NoError(t, db.Delete(&model.Channel{}, channel.Id).Error) })
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/"+strconv.Itoa(channel.Id), nil)
			ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channel.Id)}}

			GetChannel(ctx)

			var response struct {
				Success bool          `json:"success"`
				Data    model.Channel `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)
			assert.NotContains(t, recorder.Body.String(), "pat-")
			redacted := response.Data.OtherSettings
			assert.Contains(t, redacted, "[REDACTED]")
			response.Data.RestoreBalanceQueryAccessToken(&channel)
			require.NoError(t, response.Data.ValidateSettings())
			assert.Equal(t, channel.GetOtherSettings(), response.Data.GetOtherSettings())
			assert.True(t, response.Data.OtherSettingsEqualIgnoringBalanceQueryToken(&channel))

			// The transaction must restore from the current row after rotation.
			rotated := strings.ReplaceAll(settings, "pat-header-secret", "pat-rotated-secret")
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("settings", rotated).Error)
			editor := model.Channel{Id: channel.Id, Name: "after"}
			require.NoError(t, editor.UpdatePreservingBalanceQueryToken(redacted, true))
			assert.JSONEq(t, rotated, editor.OtherSettings)

			// Only the secure credential endpoint returns the request values.
			secureRecorder := httptest.NewRecorder()
			secureCtx, _ := gin.CreateTestContext(secureRecorder)
			secureCtx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/"+strconv.Itoa(channel.Id)+"/key", nil)
			secureCtx.Params = ctx.Params
			GetChannelKey(secureCtx)
			assert.Contains(t, secureRecorder.Body.String(), "pat-rotated-secret")
			assert.Contains(t, secureRecorder.Body.String(), "balance_query_request")
		})
	}
}

func TestCustomBalanceQueryRedactedAliasesPreserveEffectiveRequest(t *testing.T) {
	for _, settings := range []string{
		`{"BALANCE_QUERY":{"mode":"custom","URL":"https://upstream.example/balance?token=pat-url","HEADERS":{"Authorization":"pat-first","X-Key":"pat-extra"},"extract":"response.balance"}}`,
		`{"balance_query":{"mode":"custom","url":"https://upstream.example/balance","headers":{"Authorization":"pat-first"},"extract":"response.balance"},"Balance_Query":{"HEADERS":{"X-Key":"pat-last"}}}`,
		`{"balance_query":{"mode":"custom","url":"https://first.example/balance","URL":"https://last.example/balance","headers":{"Authorization":"pat-first"},"Headers":{"X-Key":"pat-last"},"extract":"response.balance"}}`,
	} {
		t.Run(settings, func(t *testing.T) {
			origin := model.Channel{Type: constant.ChannelTypeNewAPI, OtherSettings: settings}
			require.NoError(t, origin.ValidateSettings())
			incoming := origin
			incoming.RedactBalanceQueryAccessToken()
			assert.NotContains(t, incoming.OtherSettings, "pat-")
			incoming.RestoreBalanceQueryAccessToken(&origin)
			require.NoError(t, incoming.ValidateSettings())
			assert.Equal(t, origin.GetOtherSettings(), incoming.GetOtherSettings())
			assert.True(t, incoming.OtherSettingsEqualIgnoringBalanceQueryToken(&origin))
		})
	}
}

func TestCustomBalanceQueryCredentialsCanBeReplacedOrCleared(t *testing.T) {
	origin := model.Channel{Type: constant.ChannelTypeNewAPI, OtherSettings: `{"balance_query":{"mode":"custom","method":"POST","url":"https://upstream.example/balance","headers":{"Authorization":"pat-old","X-Key":"pat-extra"},"body":"old-body","extract":"response.balance"}}`}
	incoming := origin
	incoming.RedactBalanceQueryAccessToken()
	incoming.OtherSettings = strings.ReplaceAll(incoming.OtherSettings, `"Authorization":"[REDACTED]"`, `"Authorization":"pat-new"`)
	incoming.OtherSettings = strings.ReplaceAll(incoming.OtherSettings, `,"X-Key":"[REDACTED]"`, "")
	incoming.OtherSettings = strings.ReplaceAll(incoming.OtherSettings, `"body":"[REDACTED]"`, `"body":""`)

	incoming.RestoreBalanceQueryAccessToken(&origin)

	require.NoError(t, incoming.ValidateSettings())
	query := incoming.GetOtherSettings().BalanceQuery
	assert.Equal(t, map[string]string{"Authorization": "pat-new"}, query.Headers)
	assert.Empty(t, query.Body)
	assert.False(t, incoming.OtherSettingsEqualIgnoringBalanceQueryToken(&origin))
}

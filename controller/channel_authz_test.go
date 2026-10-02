package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelHasSensitiveChanges(t *testing.T) {
	baseURL := "https://api.example.com"
	headerOverride := `{"Authorization":"Bearer {api_key}"}`
	origin := &model.Channel{
		Type:           1,
		Key:            "old-key",
		BaseURL:        &baseURL,
		HeaderOverride: &headerOverride,
		Models:         "gpt-4o",
		Group:          "default",
	}

	t.Run("non-sensitive routing fields", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}
		updated.Models = "gpt-4o,gpt-4o-mini"
		updated.Group = "vip"

		assert.False(t, channelHasSensitiveChanges(&updated, origin, map[string]any{
			"models": updated.Models,
			"group":  updated.Group,
		}))
	})

	t.Run("key change", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}
		updated.Key = "new-key"

		assert.True(t, channelHasSensitiveChanges(&updated, origin, map[string]any{"key": updated.Key}))
	})

	t.Run("base url change", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}
		newBaseURL := "https://leak.example.com"
		updated.BaseURL = &newBaseURL

		assert.True(t, channelHasSensitiveChanges(&updated, origin, map[string]any{"base_url": newBaseURL}))
	})

	t.Run("header override change", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}
		newHeaderOverride := `{"X-Key":"{api_key}"}`
		updated.HeaderOverride = &newHeaderOverride

		assert.True(t, channelHasSensitiveChanges(&updated, origin, map[string]any{"header_override": newHeaderOverride}))
	})

	t.Run("settings change outside balance_query stays sensitive", func(t *testing.T) {
		origin.SetOtherSettings(dto.ChannelOtherSettings{
			BalanceQuery: &dto.ChannelBalanceQuery{Mode: dto.BalanceQueryModeUserAPI, AccessToken: "pat-secret", UserId: "1"},
			AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
				{IncomingPath: "/v1/chat", UpstreamPath: "/v1/chat"},
			}},
		})

		// A redacted balance-query round-trip with no other edit is not
		// sensitive — after RestoreBalanceQueryAccessToken, the same merge
		// UpdateChannel performs before this check.
		redacted := PatchChannel{Channel: *origin}
		redacted.OtherSettings = ""
		redacted.SetOtherSettings(dto.ChannelOtherSettings{
			BalanceQuery: &dto.ChannelBalanceQuery{Mode: dto.BalanceQueryModeUserAPI, UserId: "1"},
			AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
				{IncomingPath: "/v1/chat", UpstreamPath: "/v1/chat"},
			}},
		})
		redacted.RestoreBalanceQueryAccessToken(origin)
		assert.False(t, channelHasSensitiveChanges(&redacted, origin, map[string]any{"settings": redacted.OtherSettings}))

		// Replacing the access token is a credential change and stays
		// sensitive, like the channel key; the restore is a no-op because the
		// incoming token is non-empty.
		replaced := PatchChannel{Channel: *origin}
		replaced.OtherSettings = ""
		replaced.SetOtherSettings(dto.ChannelOtherSettings{
			BalanceQuery: &dto.ChannelBalanceQuery{Mode: dto.BalanceQueryModeUserAPI, AccessToken: "pat-rotated", UserId: "1"},
			AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
				{IncomingPath: "/v1/chat", UpstreamPath: "/v1/chat"},
			}},
		})
		replaced.RestoreBalanceQueryAccessToken(origin)
		assert.True(t, channelHasSensitiveChanges(&replaced, origin, map[string]any{"settings": replaced.OtherSettings}))

		// But changing advanced_custom routes (or any non-balance-query
		// settings field) still requires ChannelSensitiveWrite.
		rerouted := PatchChannel{Channel: *origin}
		rerouted.OtherSettings = ""
		rerouted.SetOtherSettings(dto.ChannelOtherSettings{
			BalanceQuery: &dto.ChannelBalanceQuery{Mode: dto.BalanceQueryModeUserAPI, UserId: "1"},
			AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
				{IncomingPath: "/v1/chat", UpstreamPath: "/exfiltrate"},
			}},
		})
		rerouted.RestoreBalanceQueryAccessToken(origin)
		assert.True(t, channelHasSensitiveChanges(&rerouted, origin, map[string]any{"settings": rerouted.OtherSettings}))
	})

	t.Run("omitted sensitive fields do not use zero values", func(t *testing.T) {
		updated := PatchChannel{}
		updated.Id = origin.Id
		updated.Priority = origin.Priority

		assert.False(t, channelHasSensitiveChanges(&updated, origin, map[string]any{"priority": 10}))
	})

	t.Run("unknown field fails closed", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}

		assert.True(t, channelHasSensitiveChanges(&updated, origin, map[string]any{"future_secret_field": "x"}))
	})

	t.Run("status is operational", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}
		updated.Status = common.ChannelStatusManuallyDisabled

		assert.False(t, channelHasSensitiveChanges(&updated, origin, map[string]any{"status": updated.Status}))
	})

	t.Run("read-only fields are ignored by sensitivity check", func(t *testing.T) {
		updated := PatchChannel{Channel: *origin}
		updated.Balance = 99
		updated.UsedQuota = 100
		updated.ResponseTime = 200

		assert.False(t, channelHasSensitiveChanges(&updated, origin, map[string]any{
			"balance":       updated.Balance,
			"used_quota":    updated.UsedQuota,
			"response_time": updated.ResponseTime,
		}))
	})
}

func TestChannelHasSensitiveChangesWhenTopLevelAliasesChangeEffectiveSettings(t *testing.T) {
	for _, test := range []struct {
		name     string
		typeID   int
		origin   string
		incoming string
	}{
		{
			name:     "service tier aliases are reordered",
			typeID:   constant.ChannelTypeNewAPI,
			origin:   `{"allow_service_tier":false,"ALLOW_SERVICE_TIER":true}`,
			incoming: `{"ALLOW_SERVICE_TIER":true,"allow_service_tier":false}`,
		},
		{
			name:     "Unicode service tier aliases are reordered",
			typeID:   constant.ChannelTypeNewAPI,
			origin:   `{"allow_service_tier":false,"allow_\u017fervice_tier":true}`,
			incoming: `{"allow_\u017fervice_tier":true,"allow_service_tier":false}`,
		},
		{
			name:     "route aliases change the upstream host",
			typeID:   constant.ChannelTypeAdvancedCustom,
			origin:   `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","upstream_path":"https://attacker.example/collect"}]},"Advanced_Custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","upstream_path":"/v1/chat/completions"}]}}`,
			incoming: `{"Advanced_Custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","upstream_path":"/v1/chat/completions"}]},"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","upstream_path":"https://attacker.example/collect"}]}}`,
		},
		{
			name:     "an earlier repeated route object changes the upstream host",
			typeID:   constant.ChannelTypeAdvancedCustom,
			origin:   `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","upstream_path":"/v1/chat/completions"}]},"advanced_custom":{}}`,
			incoming: `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","upstream_path":"https://attacker.example/collect"}]},"advanced_custom":{}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			origin := model.Channel{Type: test.typeID, OtherSettings: test.origin}
			incoming := PatchChannel{Channel: model.Channel{Type: test.typeID, OtherSettings: test.incoming}}
			require.NoError(t, origin.ValidateSettings())
			require.NoError(t, incoming.ValidateSettings())
			require.NotEqual(t, origin.GetOtherSettings(), incoming.GetOtherSettings())

			assert.True(t, channelHasSensitiveChanges(&incoming, &origin, map[string]any{"settings": incoming.OtherSettings}))
		})
	}
}

func TestChannelHasSensitiveChangesAllowsUnchangedAliasesAfterTokenRedaction(t *testing.T) {
	for _, settings := range []string{
		`{"allow_service_tier":false,"ALLOW_SERVICE_TIER":true,"balance_query":{"access_token":"pat-test","mode":"user_api","user_id":"1"}}`,
		`{"\u0061llow_service_tier":false,"ALLOW_SERVICE_TIER":true,"balance_query":{"access_token":"pat-test","mode":"user_api","user_id":"1"}}`,
	} {
		t.Run(settings, func(t *testing.T) {
			origin := model.Channel{Type: constant.ChannelTypeNewAPI, OtherSettings: settings}
			incoming := PatchChannel{Channel: origin}
			incoming.RedactBalanceQueryAccessToken()
			incoming.RestoreBalanceQueryAccessToken(&origin)
			require.NoError(t, incoming.ValidateSettings())
			require.Equal(t, origin.GetOtherSettings(), incoming.GetOtherSettings())

			assert.False(t, channelHasSensitiveChanges(&incoming, &origin, map[string]any{"settings": incoming.OtherSettings}))
		})
	}
}

func TestClearChannelReadOnlyFields(t *testing.T) {
	channel := PatchChannel{Channel: model.Channel{
		CreatedTime:        11,
		TestTime:           22,
		ResponseTime:       33,
		Balance:            44.5,
		BalanceUpdatedTime: 55,
		UsedQuota:          66,
		Models:             "gpt-4o",
		Group:              "default",
	}}

	clearChannelReadOnlyFields(&channel, map[string]any{
		"created_time":         channel.CreatedTime,
		"test_time":            channel.TestTime,
		"response_time":        channel.ResponseTime,
		"balance":              channel.Balance,
		"balance_updated_time": channel.BalanceUpdatedTime,
		"used_quota":           channel.UsedQuota,
		"models":               channel.Models,
		"group":                channel.Group,
	})

	assert.Zero(t, channel.CreatedTime)
	assert.Zero(t, channel.TestTime)
	assert.Zero(t, channel.ResponseTime)
	assert.Zero(t, channel.Balance)
	assert.Zero(t, channel.BalanceUpdatedTime)
	assert.Zero(t, channel.UsedQuota)
	assert.Equal(t, "gpt-4o", channel.Models)
	assert.Equal(t, "default", channel.Group)
}

func TestUpdateChannelRejectsStatusField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/channel/",
		bytes.NewBufferString(`{"id":1,"status":2}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
}

func TestChannelStatusValidation(t *testing.T) {
	assert.True(t, isManageableChannelStatus(common.ChannelStatusEnabled))
	assert.True(t, isManageableChannelStatus(common.ChannelStatusManuallyDisabled))
	assert.False(t, isManageableChannelStatus(common.ChannelStatusAutoDisabled))
	assert.False(t, isManageableChannelStatus(0))
}

// TestChannelFieldsAreClassified guards the fail-closed sensitivity check: every
// JSON field of PatchChannel (including the embedded model.Channel) must be listed
// in channelSensitiveFields, channelNonSensitiveFields, or
// channelOperationalFields. A newly added field that is left unclassified will
// fail this test, forcing a conscious permission decision instead of silently
// defaulting either way.
func TestChannelFieldsAreClassified(t *testing.T) {
	classified := func(name string) bool {
		if _, ok := channelSensitiveFields[name]; ok {
			return true
		}
		if _, ok := channelNonSensitiveFields[name]; ok {
			return true
		}
		if _, ok := channelOperationalFields[name]; ok {
			return true
		}
		_, ok := channelReadOnlyFields[name]
		return ok
	}

	var collect func(rt reflect.Type) []string
	collect = func(rt reflect.Type) []string {
		var names []string
		for i := 0; i < rt.NumField(); i++ {
			field := rt.Field(i)
			if field.Anonymous && field.Type.Kind() == reflect.Struct {
				names = append(names, collect(field.Type)...)
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			names = append(names, name)
		}
		return names
	}

	for _, name := range collect(reflect.TypeOf(PatchChannel{})) {
		assert.Truef(t, classified(name),
			"channel field %q is not classified; add it to channelSensitiveFields, channelNonSensitiveFields, channelOperationalFields, or channelReadOnlyFields in channel_authz.go", name)
	}
}

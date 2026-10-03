package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupBalanceTestDB initializes an in-memory SQLite database so
// channel.UpdateBalance and channel status updates have a writable model.DB.
func setupBalanceTestDB(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.User{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
	})
}

func newNewAPIBalanceChannel(t *testing.T, baseURL string, balanceQuery *dto.ChannelBalanceQuery) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Type:    constant.ChannelTypeNewAPI,
		Key:     "sk-channel-key",
		BaseURL: &baseURL,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: balanceQuery})
	// Persist so UpdateBalance writes a real row instead of logging
	// "WHERE conditions required" for a zero-id channel.
	require.NoError(t, model.DB.Create(channel).Error)
	return channel
}

func TestFetchNewAPIUserAPIBalance(t *testing.T) {
	setupBalanceTestDB(t)
	var gotPath, gotAuthorization, gotNewAPIUser string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		gotNewAPIUser = r.Header.Get("New-Api-User")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"quota":250000,"used_quota":250000}}`))
	}))
	defer server.Close()

	channel := newNewAPIBalanceChannel(t, server.URL, &dto.ChannelBalanceQuery{
		Mode:        dto.BalanceQueryModeUserAPI,
		AccessToken: "pat-secret-token",
		UserId:      "42",
	})

	balance, err := fetchNewAPIUserAPIBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
	require.NoError(t, err)
	assert.InDelta(t, 0.5, balance, 1e-9)
	assert.Equal(t, "/api/user/self", gotPath)
	assert.Equal(t, "Bearer pat-secret-token", gotAuthorization)
	assert.Equal(t, "42", gotNewAPIUser)
}

func TestFetchNewAPIUserAPIBalanceCustomQuotaPerUnit(t *testing.T) {
	setupBalanceTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"quota":1000}}`))
	}))
	defer server.Close()

	channel := newNewAPIBalanceChannel(t, server.URL, &dto.ChannelBalanceQuery{
		Mode:         dto.BalanceQueryModeUserAPI,
		AccessToken:  "pat-token",
		UserId:       "1",
		QuotaPerUnit: 1000,
	})

	balance, err := fetchNewAPIUserAPIBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
	require.NoError(t, err)
	assert.InDelta(t, 1.0, balance, 1e-9)
}

func TestFetchNewAPIUserAPIBalanceUpstreamFailure(t *testing.T) {
	setupBalanceTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"message":"token expired"}`))
	}))
	defer server.Close()

	channel := newNewAPIBalanceChannel(t, server.URL, &dto.ChannelBalanceQuery{
		Mode:        dto.BalanceQueryModeUserAPI,
		AccessToken: "pat-token",
		UserId:      "1",
	})

	_, err := fetchNewAPIUserAPIBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
	require.ErrorContains(t, err, "upstream user balance query failed")
}

func TestFetchNewAPIUserAPIBalanceMissingToken(t *testing.T) {
	setupBalanceTestDB(t)
	// Settings written before save-time validation existed can carry an empty
	// token; the query must fail locally instead of sending a bare "Bearer "
	// header to the upstream.
	channel := newNewAPIBalanceChannel(t, "https://upstream.example", &dto.ChannelBalanceQuery{
		Mode:   dto.BalanceQueryModeUserAPI,
		UserId: "1",
	})

	_, err := fetchNewAPIUserAPIBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
	require.ErrorContains(t, err, "access token is missing")
}

func TestFetchNewAPIUserAPIBalanceMissingUserID(t *testing.T) {
	setupBalanceTestDB(t)
	// Legacy settings can also lack the user id; the query must fail locally
	// instead of sending an empty New-Api-User header to the upstream.
	channel := newNewAPIBalanceChannel(t, "https://upstream.example", &dto.ChannelBalanceQuery{
		Mode:        dto.BalanceQueryModeUserAPI,
		AccessToken: "pat-token",
	})

	_, err := fetchNewAPIUserAPIBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
	require.ErrorContains(t, err, "user id is missing")
}

func TestFetchCustomTemplateBalance(t *testing.T) {
	setupBalanceTestDB(t)
	var gotMethod, gotAuthorization, gotCustomHeader, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuthorization = r.Header.Get("Authorization")
		gotCustomHeader = r.Header.Get("X-Custom-Auth")
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			gotBody = string(body)
		}
		_, _ = w.Write([]byte(`{"data":{"available_balance":"12.5","currency":"CNY"}}`))
	}))
	defer server.Close()

	channel := newNewAPIBalanceChannel(t, server.URL, &dto.ChannelBalanceQuery{
		Mode: dto.BalanceQueryModeCustom,
		URL:  "{base_url}/v1/custom/balance",
		Headers: map[string]string{
			"X-Custom-Auth": "token-{key}",
		},
		Extract: `float(json("data.available_balance")) / 7.25`,
	})

	balance, err := fetchCustomTemplateBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
	require.NoError(t, err)
	assert.InDelta(t, 12.5/7.25, balance, 1e-9)
	assert.Equal(t, http.MethodGet, gotMethod)
	// No Authorization is configured and none is added implicitly: the
	// channel key travels only where the template explicitly places it.
	assert.Empty(t, gotAuthorization)
	assert.Equal(t, "token-sk-channel-key", gotCustomHeader)
	assert.Empty(t, gotBody)
}

func TestFetchCustomTemplateBalanceRelativePathKeepsBasePathPrefix(t *testing.T) {
	setupBalanceTestDB(t)
	var gotPath string
	// The upstream is mounted under a path prefix; a relative balance-query
	// path must resolve like "{base_url}/v1/balance", not against the host root.
	mux := http.NewServeMux()
	mux.HandleFunc("/newapi/v1/balance", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"balance":5}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	channel := newNewAPIBalanceChannel(t, server.URL+"/newapi", &dto.ChannelBalanceQuery{
		Mode:    dto.BalanceQueryModeCustom,
		URL:     "/v1/balance",
		Extract: `response.balance`,
	})

	balance, err := fetchCustomTemplateBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
	require.NoError(t, err)
	assert.InDelta(t, 5.0, balance, 1e-9)
	assert.Equal(t, "/newapi/v1/balance", gotPath)
}

func TestFetchCustomTemplateBalancePostBodyAndResponseEnv(t *testing.T) {
	setupBalanceTestDB(t)
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = w.Write([]byte(`{"balance":9.75}`))
	}))
	defer server.Close()

	channel := newNewAPIBalanceChannel(t, server.URL, &dto.ChannelBalanceQuery{
		Mode:   dto.BalanceQueryModeCustom,
		Method: "POST",
		URL:    "/v1/balance",
		Body:   `{"key":"{key}"}`,
		Headers: map[string]string{
			"Authorization": "Bearer {key}",
		},
		Extract: `response.balance`,
	})

	balance, err := fetchCustomTemplateBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
	require.NoError(t, err)
	assert.InDelta(t, 9.75, balance, 1e-9)
	assert.JSONEq(t, `{"key":"sk-channel-key"}`, gotBody)
}

func TestFetchCustomTemplateBalanceExpressionFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"balance":1,"data":{"name":"gold plan"}}`))
	}))
	defer server.Close()

	tests := []struct {
		name    string
		extract string
		wantErr string
	}{
		{name: "non-numeric result", extract: `json("data.name")`, wantErr: "run error"},
		{name: "compile error", extract: `response.balance +`, wantErr: "compile error"},
		{name: "run error", extract: `response.balance.missing.deep`, wantErr: "run error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupBalanceTestDB(t)
			channel := newNewAPIBalanceChannel(t, server.URL, &dto.ChannelBalanceQuery{
				Mode:    dto.BalanceQueryModeCustom,
				URL:     server.URL + "/v1/balance",
				Extract: test.extract,
			})
			_, err := fetchCustomTemplateBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestFetchCustomTemplateBalanceErrorRedactsKey(t *testing.T) {
	setupBalanceTestDB(t)
	// A closed server makes the HTTP request fail; the error must not leak
	// the channel key from the templated URL.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	serverURL := server.URL
	server.Close()

	channel := newNewAPIBalanceChannel(t, serverURL, &dto.ChannelBalanceQuery{
		Mode: dto.BalanceQueryModeCustom,
		URL:  "{base_url}/v1/balance",
		Headers: map[string]string{
			"Authorization": "Bearer {key}",
		},
		Extract: `response.balance`,
	})

	_, err := fetchCustomTemplateBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sk-channel-key")
}

func TestFetchCustomTemplateBalanceRedactsPercentEncodedKey(t *testing.T) {
	setupBalanceTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"balance":"invalid key sk%20key%2F%26"}`))
	}))
	defer server.Close()
	channel := newNewAPIBalanceChannel(t, server.URL, &dto.ChannelBalanceQuery{
		Mode: dto.BalanceQueryModeCustom, URL: "/balance?key={key}", Extract: `float(json("balance"))`,
	})
	channel.Key = "sk key/&"

	_, err := fetchCustomTemplateBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)

	require.ErrorContains(t, err, "balance extract expression run error")
	assert.NotContains(t, err.Error(), "sk%20key%2F%26")
}

func TestUpdateNewAPIChannelBalanceDisabledDefault(t *testing.T) {
	setupBalanceTestDB(t)
	// When balance query is nil (or explicitly disabled), it defaults to disabled mode
	channelNil := newNewAPIBalanceChannel(t, "https://upstream.example", nil)
	handled, _, err := updateNewAPIChannelBalance(channelNil)
	require.Error(t, err)
	assert.Equal(t, "余额查询已关闭", err.Error())
	assert.True(t, handled)

	channelDisabled := newNewAPIBalanceChannel(t, "https://upstream.example", &dto.ChannelBalanceQuery{
		Mode: dto.BalanceQueryModeDisabled,
	})
	handled, _, err = updateNewAPIChannelBalance(channelDisabled)
	require.Error(t, err)
	assert.Equal(t, "余额查询已关闭", err.Error())
	assert.True(t, handled)
}

func TestUpdateNewAPIChannelBalanceSubscriptionFlowsThrough(t *testing.T) {
	setupBalanceTestDB(t)
	channel := newNewAPIBalanceChannel(t, "https://upstream.example", &dto.ChannelBalanceQuery{
		Mode: dto.BalanceQueryModeSubscription,
	})

	handled, _, err := updateNewAPIChannelBalance(channel)
	require.NoError(t, err)
	assert.False(t, handled)
}

func TestUpdateNewAPIChannelBalanceSubscriptionTimesOut(t *testing.T) {
	setupBalanceTestDB(t)
	previousTimeout := balanceQueryRequestTimeout
	balanceQueryRequestTimeout = 50 * time.Millisecond
	t.Cleanup(func() { balanceQueryRequestTimeout = previousTimeout })

	requestStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
		if r.URL.Path == "/v1/dashboard/billing/subscription" {
			_, _ = w.Write([]byte(`{"has_payment_method":true,"hard_limit_usd":10}`))
			return
		}
		_, _ = w.Write([]byte(`{"total_usage":0}`))
	}))
	defer server.Close()
	defer close(release)

	channel := newNewAPIBalanceChannel(t, server.URL, &dto.ChannelBalanceQuery{
		Mode: dto.BalanceQueryModeSubscription,
	})
	start := time.Now()
	_, err := updateStandardChannelBalance(channel)
	require.Error(t, err)
	require.NotEmpty(t, requestStarted)
	assert.Less(t, time.Since(start), 500*time.Millisecond)
}

func TestFetchNewAPIUserAPIBalanceRejectsNonFiniteResult(t *testing.T) {
	setupBalanceTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1e9 / 1e-300 overflows float64 (max ~1.8e308) to +Inf; a smaller
		// quota would stay finite and never reach the guard.
		_, _ = w.Write([]byte(`{"success":true,"data":{"quota":1000000000}}`))
	}))
	defer server.Close()

	channel := newNewAPIBalanceChannel(t, server.URL, &dto.ChannelBalanceQuery{
		Mode:        dto.BalanceQueryModeUserAPI,
		AccessToken: "pat-token",
		UserId:      "1",
		// Legacy settings can carry a near-zero divisor that overflows the
		// division; the query must fail instead of storing an Inf balance.
		QuotaPerUnit: 1e-300,
	})

	_, err := fetchNewAPIUserAPIBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
	require.ErrorContains(t, err, "non-finite")
}

func TestUpdateAllChannelsBalanceDoesNotDisableNewAPIChannelsOnZeroBalance(t *testing.T) {
	setupBalanceTestDB(t)
	// Shrink the inter-channel sleep so the loop finishes immediately.
	previousInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() {
		common.RequestInterval = previousInterval
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// user_api mode: a zero-quota account that still answers 200.
		_, _ = w.Write([]byte(`{"success":true,"data":{"quota":0}}`))
	}))
	defer server.Close()

	autoBan := 1
	channel := &model.Channel{
		Type:    constant.ChannelTypeNewAPI,
		Key:     "sk-channel-key",
		Name:    "zero-balance new api",
		Status:  common.ChannelStatusEnabled,
		AutoBan: &autoBan,
		BaseURL: &server.URL,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: &dto.ChannelBalanceQuery{
		Mode:        dto.BalanceQueryModeUserAPI,
		AccessToken: "pat-token",
		UserId:      "1",
	}})
	require.NoError(t, model.DB.Create(channel).Error)

	require.NoError(t, updateAllChannelsBalance())

	var reloaded model.Channel
	require.NoError(t, model.DB.First(&reloaded, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status,
		"New API channels must stay enabled even when the queried balance is zero")
	assert.InDelta(t, 0.0, reloaded.Balance, 1e-9)
}

func TestUpdateAllChannelsBalanceDisablesSubscriptionModeNewAPIOnZeroBalance(t *testing.T) {
	setupBalanceTestDB(t)
	previousInterval := common.RequestInterval
	common.RequestInterval = 0
	// DisableChannel notifies the root user; force the in-memory notify limiter
	// so the test never touches a Redis client.
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RequestInterval = previousInterval
		common.RedisEnabled = previousRedisEnabled
	})

	// subscription mode: the OpenAI dashboard endpoints answer zero balance,
	// which describes the channel key itself, so the channel must be banned.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/dashboard/billing/subscription":
			_, _ = w.Write([]byte(`{"object":"billing_subscription","has_payment_method":false,"hard_limit_usd":0}`))
		case "/v1/dashboard/billing/usage":
			_, _ = w.Write([]byte(`{"object":"list","total_usage":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	autoBan := 1
	channel := &model.Channel{
		Type:    constant.ChannelTypeNewAPI,
		Key:     "sk-channel-key",
		Name:    "zero-balance subscription new api",
		Status:  common.ChannelStatusEnabled,
		AutoBan: &autoBan,
		BaseURL: &server.URL,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: &dto.ChannelBalanceQuery{
		Mode: dto.BalanceQueryModeSubscription,
	}})
	require.NoError(t, model.DB.Create(channel).Error)

	require.NoError(t, updateAllChannelsBalance())

	var reloaded model.Channel
	require.NoError(t, model.DB.First(&reloaded, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, reloaded.Status,
		"subscription-mode New API channels must still be auto-disabled on zero balance")
}

func TestRedactBalanceQueryAccessToken(t *testing.T) {
	setupBalanceTestDB(t)
	channel := &model.Channel{
		Type:    constant.ChannelTypeNewAPI,
		Key:     "sk-channel-key",
		BaseURL: func(v string) *string { return &v }("https://upstream.example"),
	}
	// Include a key the settings DTO does not model: redaction must be a
	// surgical edit of balance_query.access_token, not a DTO re-marshal that
	// would silently drop unknown keys.
	channel.OtherSettings = `{"openrouter_enterprise":false,"future_field":{"nested":[1,2,3]},"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"}}`
	channel.Id = 0
	require.NoError(t, model.DB.Create(channel).Error)

	// Redaction must clear the token in the serialized view without
	// persisting the redaction to the database.
	clearChannelInfo(channel)
	settings := channel.GetOtherSettings()
	require.NotNil(t, settings.BalanceQuery)
	assert.Empty(t, settings.BalanceQuery.AccessToken)
	assert.Equal(t, "1", settings.BalanceQuery.UserId)
	var raw map[string]json.RawMessage
	require.NoError(t, common.Unmarshal([]byte(channel.OtherSettings), &raw))
	assert.JSONEq(t, `{"nested":[1,2,3]}`, string(raw["future_field"]),
		"keys unknown to the settings DTO must survive redaction")
	assert.JSONEq(t, `{"mode":"user_api","user_id":"1"}`, string(raw["balance_query"]))

	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	storedSettings := stored.GetOtherSettings()
	require.NotNil(t, storedSettings.BalanceQuery)
	assert.Equal(t, "pat-secret", storedSettings.BalanceQuery.AccessToken,
		"redaction must only affect the API copy, never the stored settings")

	// An update that keeps user_api mode with an empty token must restore the
	// stored token; switching to custom mode must not.
	channel.RestoreBalanceQueryAccessToken(&stored)
	assert.Equal(t, "pat-secret", channel.GetOtherSettings().BalanceQuery.AccessToken)
}

func TestBalanceQueryEqualsIgnoringAccessToken(t *testing.T) {
	origin := &model.Channel{Type: constant.ChannelTypeNewAPI}
	origin.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: &dto.ChannelBalanceQuery{
		Mode:        dto.BalanceQueryModeUserAPI,
		AccessToken: "pat-secret",
		UserId:      "1",
	}})

	// Round-trip of the redacted copy with the same values is not a change,
	// once the stored token is merged back the way UpdateChannel does.
	roundTripped := &model.Channel{Type: constant.ChannelTypeNewAPI}
	roundTripped.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: &dto.ChannelBalanceQuery{
		Mode:   dto.BalanceQueryModeUserAPI,
		UserId: "1",
	}})
	roundTripped.RestoreBalanceQueryAccessToken(origin)
	assert.True(t, roundTripped.OtherSettingsEqualIgnoringBalanceQueryToken(origin))

	// Replacing the token with a different value is a credential change and
	// stays sensitive (like the channel key); the restore is a no-op because
	// the incoming token is non-empty.
	replaced := &model.Channel{Type: constant.ChannelTypeNewAPI}
	replaced.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: &dto.ChannelBalanceQuery{
		Mode:        dto.BalanceQueryModeUserAPI,
		AccessToken: "pat-rotated",
		UserId:      "1",
	}})
	replaced.RestoreBalanceQueryAccessToken(origin)
	assert.False(t, replaced.OtherSettingsEqualIgnoringBalanceQueryToken(origin))

	// A token-less user_api payload that was never restored does not compare
	// equal either: only UpdateChannel may inject the stored token.
	redactedNoRestore := &model.Channel{Type: constant.ChannelTypeNewAPI}
	redactedNoRestore.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: &dto.ChannelBalanceQuery{
		Mode:   dto.BalanceQueryModeUserAPI,
		UserId: "1",
	}})
	assert.False(t, redactedNoRestore.OtherSettingsEqualIgnoringBalanceQueryToken(origin))

	// Any other field difference is a change.
	edited := &model.Channel{Type: constant.ChannelTypeNewAPI}
	edited.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: &dto.ChannelBalanceQuery{
		Mode:   dto.BalanceQueryModeUserAPI,
		UserId: "2",
	}})
	edited.RestoreBalanceQueryAccessToken(origin)
	assert.False(t, edited.OtherSettingsEqualIgnoringBalanceQueryToken(origin))
}

func TestLegacyNewAPIDefaultBalanceQueryIsNotSensitive(t *testing.T) {
	// A legacy New API channel stored no balance_query at all. A client that
	// injects the bare disabled default into the settings payload must
	// not turn an otherwise-unrelated edit (e.g. rename) into a sensitive
	// settings change requiring ChannelSensitiveWrite.
	origin := &model.Channel{Type: constant.ChannelTypeNewAPI}
	origin.OtherSettings = `{"openrouter_enterprise":false}`

	injectedDefault := &model.Channel{Type: constant.ChannelTypeNewAPI}
	injectedDefault.OtherSettings = `{"openrouter_enterprise":false,"balance_query":{"mode":"disabled"}}`
	assert.True(t, injectedDefault.OtherSettingsEqualIgnoringBalanceQueryToken(origin))
	assert.False(t, channelHasSensitiveChanges(
		&PatchChannel{Channel: *injectedDefault},
		origin,
		map[string]any{"settings": injectedDefault.OtherSettings, "name": "renamed"},
	))

	// The same holds when the stored side carries the default and the
	// incoming side drops it entirely.
	storedDefault := &model.Channel{Type: constant.ChannelTypeNewAPI}
	storedDefault.OtherSettings = `{"balance_query":{"mode":"disabled"}}`
	noBalanceQuery := &model.Channel{Type: constant.ChannelTypeNewAPI}
	noBalanceQuery.OtherSettings = `{}`
	assert.True(t, noBalanceQuery.OtherSettingsEqualIgnoringBalanceQueryToken(storedDefault))
}

func TestOtherSettingsCompareKeepsUnknownKeysSensitive(t *testing.T) {
	// The comparison must run on the raw settings JSON, not the settings DTO:
	// keys the DTO does not model stay visible, so tampering with one is a
	// sensitive change requiring ChannelSensitiveWrite.
	origin := &model.Channel{Type: constant.ChannelTypeNewAPI}
	origin.OtherSettings = `{"future_field":{"nested":[1,2,3]},"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"}}`

	tampered := &model.Channel{Type: constant.ChannelTypeNewAPI}
	tampered.OtherSettings = `{"future_field":{"nested":[9,9,9]},"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"}}`
	assert.False(t, tampered.OtherSettingsEqualIgnoringBalanceQueryToken(origin))
	assert.True(t, channelHasSensitiveChanges(
		&PatchChannel{Channel: *tampered},
		origin,
		map[string]any{"settings": tampered.OtherSettings},
	))

	// A redacted round-trip that preserves the unknown keys (in any key
	// order) still compares equal, once the stored token is merged back the
	// way UpdateChannel does.
	roundTripped := &model.Channel{Type: constant.ChannelTypeNewAPI}
	roundTripped.OtherSettings = `{"balance_query":{"user_id":"1","mode":"user_api"},"future_field":{"nested":[1,2,3]}}`
	roundTripped.RestoreBalanceQueryAccessToken(origin)
	assert.True(t, roundTripped.OtherSettingsEqualIgnoringBalanceQueryToken(origin))
}

func TestRedactBalanceQueryAccessTokenFailsClosed(t *testing.T) {
	tests := []struct {
		name         string
		settings     string
		wantSettings string
	}{
		{
			name:         "empty settings stay empty",
			settings:     "",
			wantSettings: "",
		},
		{
			name:         "unparseable settings JSON clears the API copy entirely",
			settings:     `{"balance_query":{"access_token":"pat-secret"},"future_field":`,
			wantSettings: "",
		},
		{
			name:         "unreadable balance_query object is dropped",
			settings:     `{"future_field":{"a":1},"balance_query":"corrupt"}`,
			wantSettings: `{"future_field":{"a":1}}`,
		},
		{
			name:         "non-string access_token is dropped whatever its type",
			settings:     `{"balance_query":{"mode":"user_api","access_token":123,"user_id":"1"}}`,
			wantSettings: `{"balance_query":{"mode":"user_api","user_id":"1"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{Type: constant.ChannelTypeNewAPI}
			channel.OtherSettings = test.settings
			channel.RedactBalanceQueryAccessToken()
			assert.Equal(t, test.wantSettings, channel.OtherSettings)
			assert.NotContains(t, channel.OtherSettings, "pat-secret")
		})
	}
}

func TestOtherSettingsCompareKeepsBigIntegerPrecision(t *testing.T) {
	// The comparison must keep raw JSON fidelity: two different big integers
	// that collapse to the same float64 are still a settings change requiring
	// ChannelSensitiveWrite.
	origin := &model.Channel{Type: constant.ChannelTypeNewAPI}
	origin.OtherSettings = `{"future_field":{"big":12345678901234567890},"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"}}`

	tampered := &model.Channel{Type: constant.ChannelTypeNewAPI}
	tampered.OtherSettings = `{"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"},"future_field":{"big":12345678901234567168}}`
	assert.False(t, tampered.OtherSettingsEqualIgnoringBalanceQueryToken(origin),
		"12345678901234567890 and 12345678901234567168 collapse to the same float64 but are different settings")

	// The same settings in a different key order still compare equal.
	identical := &model.Channel{Type: constant.ChannelTypeNewAPI}
	identical.OtherSettings = `{"balance_query":{"user_id":"1","mode":"user_api","access_token":"pat-secret"},"future_field":{"big":12345678901234567890}}`
	assert.True(t, identical.OtherSettingsEqualIgnoringBalanceQueryToken(origin))
}

func TestOtherSettingsCompareDetectsNonStringTokenSwap(t *testing.T) {
	// A non-string access_token has no semantic string form; the raw bytes
	// must still be compared so swapping one stays a credential change.
	origin := &model.Channel{Type: constant.ChannelTypeNewAPI}
	origin.OtherSettings = `{"balance_query":{"mode":"user_api","access_token":123,"user_id":"1"}}`

	swapped := &model.Channel{Type: constant.ChannelTypeNewAPI}
	swapped.OtherSettings = `{"balance_query":{"mode":"user_api","access_token":456,"user_id":"1"}}`
	assert.False(t, swapped.OtherSettingsEqualIgnoringBalanceQueryToken(origin))

	same := &model.Channel{Type: constant.ChannelTypeNewAPI}
	same.OtherSettings = `{"balance_query":{"user_id":"1","mode":"user_api","access_token":123}}`
	assert.True(t, same.OtherSettingsEqualIgnoringBalanceQueryToken(origin))
}

func TestOtherSettingsCompareDropsUnreadableBalanceQuery(t *testing.T) {
	// An unreadable balance_query object carries no comparable credential and
	// is dropped from the comparison the same way redaction drops it from
	// responses: an unchanged round-trip stays editable while a real config
	// change stays sensitive.
	origin := &model.Channel{Type: constant.ChannelTypeNewAPI}
	origin.OtherSettings = `{"balance_query":"corrupt"}`

	roundTripped := &model.Channel{Type: constant.ChannelTypeNewAPI}
	roundTripped.OtherSettings = ``
	assert.True(t, roundTripped.OtherSettingsEqualIgnoringBalanceQueryToken(origin))

	replaced := &model.Channel{Type: constant.ChannelTypeNewAPI}
	replaced.OtherSettings = `{"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"}}`
	assert.False(t, replaced.OtherSettingsEqualIgnoringBalanceQueryToken(origin))
}

func TestRestoreBalanceQueryAccessTokenPreservesUnknownKeys(t *testing.T) {
	origin := &model.Channel{Type: constant.ChannelTypeNewAPI}
	origin.OtherSettings = `{"future_field":{"big":12345678901234567890},"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"}}`

	// user_api with an empty token inherits the stored token, and the merge
	// keeps settings keys the DTO does not model.
	incoming := &model.Channel{Type: constant.ChannelTypeNewAPI}
	incoming.OtherSettings = `{"future_field":{"big":12345678901234567890},"balance_query":{"mode":"user_api","user_id":"1"}}`
	incoming.RestoreBalanceQueryAccessToken(origin)
	settings := incoming.GetOtherSettings()
	require.NotNil(t, settings.BalanceQuery)
	assert.Equal(t, "pat-secret", settings.BalanceQuery.AccessToken)
	var raw map[string]json.RawMessage
	require.NoError(t, common.Unmarshal([]byte(incoming.OtherSettings), &raw))
	// Byte-exact, not just JSONEq: a map[string]interface{} round-trip would
	// rewrite the big integer as a float64 (1.2345678901234567e+19).
	assert.Equal(t, `{"big":12345678901234567890}`, string(raw["future_field"]),
		"keys unknown to the settings DTO must survive the token restore byte-exactly")
	assert.JSONEq(t, `{"mode":"user_api","access_token":"pat-secret","user_id":"1"}`, string(raw["balance_query"]))

	// Non-user_api modes never inherit a user_api token.
	customMode := &model.Channel{Type: constant.ChannelTypeNewAPI}
	customMode.OtherSettings = `{"balance_query":{"mode":"custom","url":"/v1/balance","extract":"response.balance"}}`
	customMode.RestoreBalanceQueryAccessToken(origin)
	assert.NotContains(t, customMode.OtherSettings, "pat-secret")
}

func TestFetchCustomTemplateBalanceEscapesKeyInURL(t *testing.T) {
	setupBalanceTestDB(t)
	var gotRawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"balance":1}`))
	}))
	defer server.Close()

	// A key containing URL-structural characters must not corrupt the request
	// target: the {key} placeholder is path-escaped inside URLs, while header
	// and body placeholders keep the raw key.
	channel := &model.Channel{
		Type:    constant.ChannelTypeNewAPI,
		Key:     "sk key#frag?x",
		BaseURL: &server.URL,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: &dto.ChannelBalanceQuery{
		Mode:    dto.BalanceQueryModeCustom,
		URL:     "{base_url}/v1/balance?key={key}",
		Headers: map[string]string{"X-Debug": "{key}"},
		Extract: `response.balance`,
	}})
	require.NoError(t, model.DB.Create(channel).Error)

	balance, err := fetchCustomTemplateBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
	require.NoError(t, err)
	assert.InDelta(t, 1.0, balance, 1e-9)
	assert.Equal(t, "key=sk%20key%23frag%3Fx", gotRawQuery)
}

func TestFetchCustomTemplateBalancePreservesKeyInPathAndQuery(t *testing.T) {
	for _, key := range []string{"sk+a&scope=x", "sk key/#?=&+%"} {
		t.Run(key, func(t *testing.T) {
			setupBalanceTestDB(t)
			var gotPath, gotKey, gotHeader string
			var queryCount int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotKey = r.URL.Query().Get("api_key")
				gotHeader = r.Header.Get("X-Key")
				queryCount = len(r.URL.Query())
				_, _ = w.Write([]byte(`{"balance":1}`))
			}))
			defer server.Close()
			channel := newNewAPIBalanceChannel(t, server.URL, &dto.ChannelBalanceQuery{
				Mode: dto.BalanceQueryModeCustom, URL: "{base_url}/keys/{key}/balance?api_key={key}",
				Headers: map[string]string{"X-Key": "{key}"}, Extract: "response.balance",
			})
			channel.Key = key

			_, err := fetchCustomTemplateBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)

			require.NoError(t, err)
			assert.Equal(t, "/keys/"+key+"/balance", gotPath)
			assert.Equal(t, key, gotKey)
			assert.Equal(t, key, gotHeader)
			assert.Equal(t, 1, queryCount, "a key must not inject additional query parameters")
		})
	}
}

func TestFetchCustomTemplateBalanceHostHeader(t *testing.T) {
	setupBalanceTestDB(t)
	var gotHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		_, _ = w.Write([]byte(`{"balance":3.5}`))
	}))
	defer server.Close()

	channel := newNewAPIBalanceChannel(t, server.URL, &dto.ChannelBalanceQuery{
		Mode:    dto.BalanceQueryModeCustom,
		URL:     "/v1/balance",
		Headers: map[string]string{"Host": "upstream.internal.example"},
		Extract: `response.balance`,
	})

	balance, err := fetchCustomTemplateBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
	require.NoError(t, err)
	assert.InDelta(t, 3.5, balance, 1e-9)
	assert.Equal(t, "upstream.internal.example", gotHost,
		"a configured Host header must reach the upstream as the request Host")
}

func TestUpdatePreservingBalanceQueryTokenSurvivesConcurrentRotation(t *testing.T) {
	setupBalanceTestDB(t)
	channel := &model.Channel{
		Type:    constant.ChannelTypeNewAPI,
		Key:     "sk-channel-key",
		Name:    "before",
		BaseURL: func(v string) *string { return &v }("https://upstream.example"),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: &dto.ChannelBalanceQuery{
		Mode:        dto.BalanceQueryModeUserAPI,
		AccessToken: "pat-old",
		UserId:      "1",
	}})
	require.NoError(t, model.DB.Create(channel).Error)

	// The editor validated its request against the pat-old snapshot. Before
	// the write lands, a concurrent update rotates the stored token; the
	// editor's empty-token payload must keep the rotated token instead of
	// resurrecting the stale one.
	editor := &model.Channel{Id: channel.Id, Type: constant.ChannelTypeNewAPI, Name: "after"}
	editor.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: &dto.ChannelBalanceQuery{
		Mode:   dto.BalanceQueryModeUserAPI,
		UserId: "1",
	}})
	editorSettings := editor.OtherSettings

	rotator := &model.Channel{Id: channel.Id}
	rotator.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: &dto.ChannelBalanceQuery{
		Mode:        dto.BalanceQueryModeUserAPI,
		AccessToken: "pat-rotated",
		UserId:      "1",
	}})
	require.NoError(t, model.DB.Model(rotator).Update("settings", rotator.OtherSettings).Error)

	require.NoError(t, editor.UpdatePreservingBalanceQueryToken(editorSettings, true))

	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "after", stored.Name, "the editor's own fields must be applied")
	storedSettings := stored.GetOtherSettings()
	require.NotNil(t, storedSettings.BalanceQuery)
	assert.Equal(t, "pat-rotated", storedSettings.BalanceQuery.AccessToken,
		"a concurrent rotation must not be reverted by a stale snapshot")
	assert.Equal(t, "1", storedSettings.BalanceQuery.UserId)
	assert.Equal(t, "pat-rotated", editor.GetOtherSettings().BalanceQuery.AccessToken,
		"the in-memory channel must carry the freshly merged settings too")

	// A token supplied by this very edit still wins over the stored one.
	replacing := &model.Channel{Id: channel.Id, Type: constant.ChannelTypeNewAPI, Name: "replaced"}
	replacing.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: &dto.ChannelBalanceQuery{
		Mode:        dto.BalanceQueryModeUserAPI,
		AccessToken: "pat-new",
		UserId:      "1",
	}})
	replacingSettings := replacing.OtherSettings
	require.NoError(t, replacing.UpdatePreservingBalanceQueryToken(replacingSettings, true))

	var reloaded model.Channel
	require.NoError(t, model.DB.First(&reloaded, channel.Id).Error)
	assert.Equal(t, "pat-new", reloaded.GetOtherSettings().BalanceQuery.AccessToken)
}

func TestUpdatePreservingBalanceQueryTokenRejectsConcurrentlyClearedToken(t *testing.T) {
	setupBalanceTestDB(t)
	channel := &model.Channel{
		Type:    constant.ChannelTypeNewAPI,
		Key:     "sk-channel-key",
		Name:    "before",
		BaseURL: func(v string) *string { return &v }("https://upstream.example"),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: &dto.ChannelBalanceQuery{
		Mode:        dto.BalanceQueryModeUserAPI,
		AccessToken: "pat-old",
		UserId:      "1",
	}})
	require.NoError(t, model.DB.Create(channel).Error)

	// A concurrent update switched the stored settings away from user_api,
	// clearing the token: keep-existing has nothing left to keep, so the
	// editor's empty-token payload must be rejected instead of persisting
	// tokenless user_api settings.
	require.NoError(t, model.DB.Model(&model.Channel{Id: channel.Id}).Update("settings", `{}`).Error)

	editor := &model.Channel{Id: channel.Id, Type: constant.ChannelTypeNewAPI, Name: "after"}
	editorSettings := `{"balance_query":{"mode":"user_api","user_id":"1"}}`
	err := editor.UpdatePreservingBalanceQueryToken(editorSettings, true)
	require.ErrorContains(t, err, "access_token")

	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "before", stored.Name, "the rejected update must roll back completely")
	assert.Equal(t, `{}`, stored.OtherSettings)
}

func TestUpdatePreservingBalanceQueryTokenStripsBalanceQueryForNonNewAPI(t *testing.T) {
	setupBalanceTestDB(t)
	// The controller snapshots the raw incoming settings before its
	// validation-time strip, so the write path must strip a stale
	// balance_query block from a non-NewAPI channel itself.
	channel := &model.Channel{
		Type:    constant.ChannelTypeOpenAI,
		Key:     "sk-channel-key",
		Name:    "type changed",
		Group:   "default",
		Models:  "gpt-5",
		BaseURL: func(v string) *string { return &v }("https://upstream.example"),
	}
	require.NoError(t, model.DB.Create(channel).Error)

	rawSettings := `{"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"},"openrouter_enterprise":false}`
	require.NoError(t, channel.UpdatePreservingBalanceQueryToken(rawSettings, true))

	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.NotContains(t, stored.OtherSettings, "balance_query")
	assert.NotContains(t, stored.OtherSettings, "pat-secret")
	// Compare the whole stored settings object: indexing into a raw map and
	// handing JSONEq just the openrouter_enterprise value would compare a
	// bare boolean against a JSON object, which can never be equal.
	assert.JSONEq(t, `{"openrouter_enterprise":false}`, stored.OtherSettings,
		"keys outside balance_query must survive the strip")
}

func TestFetchCustomTemplateBalanceSlowUpstreamTimesOut(t *testing.T) {
	setupBalanceTestDB(t)
	// The balance query must not inherit the relay client's RELAY_TIMEOUT=0
	// (no timeout): a hanging endpoint would otherwise stall the serial
	// updateAllChannelsBalance loop forever. Shrink the timeout so the test
	// stays fast; the constant is restored afterwards.
	previousTimeout := balanceQueryRequestTimeout
	balanceQueryRequestTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		balanceQueryRequestTimeout = previousTimeout
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client side of the connection goes away (i.e. our
		// timeout fired), so the handler never answers in time.
		<-r.Context().Done()
	}))
	defer server.Close()

	channel := newNewAPIBalanceChannel(t, server.URL, &dto.ChannelBalanceQuery{
		Mode:    dto.BalanceQueryModeCustom,
		URL:     "/v1/balance",
		Extract: `response.balance`,
	})

	start := time.Now()
	_, err := fetchCustomTemplateBalance(channel, channel.GetOtherSettings().BalanceQuery, channel.Key)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second,
		"a hanging balance endpoint must fail at the request timeout, not wait indefinitely")
}

func TestUpdateChannelBalanceDisabledReturnsMessage(t *testing.T) {
	setupBalanceTestDB(t)
	channel := newNewAPIBalanceChannel(t, "https://upstream.example", nil)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channel.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/update_balance/"+strconv.Itoa(channel.Id), nil)

	UpdateChannelBalance(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "余额查询已关闭", response.Message)
}

func TestUpdateAllChannelsBalanceSkipsDisabledNewAPI(t *testing.T) {
	setupBalanceTestDB(t)
	previousInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() {
		common.RequestInterval = previousInterval
	})

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	channel := newNewAPIBalanceChannel(t, server.URL, nil)
	channel.Status = common.ChannelStatusEnabled
	require.NoError(t, model.DB.Save(channel).Error)

	require.NoError(t, updateAllChannelsBalance())
	assert.False(t, called, "disabled New API channel balance queries must not reach the upstream")
}

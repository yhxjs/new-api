package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBalanceQueryCaseVariantsRoundTrip(t *testing.T) {
	for _, settings := range []string{
		`{"BALANCE_QUERY":{"mode":"user_api","access_token":"pat-test","user_id":"1"}}`,
		`{"balance_query":{"mode":"user_api","ACCESS_TOKEN":"pat-test","user_id":"1"}}`,
		`{"balance_query":{"mode":"user_api","access_token":"pat-first","Access_Token":"pat-last","user_id":"1"}}`,
		`{"balance_query":{"mode":"user_api","access_token":"pat-first","user_id":"1"},"Balance_Query":{"mode":"user_api","ACCESS_TOKEN":"pat-last","user_id":"1"}}`,
		`{"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1"},"balance_query":{"quota_per_unit":500000}}`,
		`{"balance_query":{"quota_per_unit":1000},"Balance_Query":null,"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1"}}`,
	} {
		t.Run(settings, func(t *testing.T) {
			origin := Channel{Type: constant.ChannelTypeNewAPI, OtherSettings: settings}
			require.NoError(t, origin.ValidateSettings())
			incoming := origin
			incoming.RedactBalanceQueryAccessToken()

			incoming.RestoreBalanceQueryAccessToken(&origin)

			require.NoError(t, incoming.ValidateSettings())
			assert.Equal(t, origin.GetOtherSettings().BalanceQuery, incoming.GetOtherSettings().BalanceQuery)
			assert.JSONEq(t, origin.OtherSettings, incoming.OtherSettings)
			assert.True(t, incoming.OtherSettingsEqualIgnoringBalanceQueryToken(&origin))
		})
	}
}

func TestBalanceQueryCaseVariantTokenChangesRemainSensitive(t *testing.T) {
	for _, settings := range []string{
		`{"BALANCE_QUERY":{"mode":"user_api","access_token":"pat-original","user_id":"1"}}`,
		`{"balance_query":{"mode":"user_api","ACCESS_TOKEN":"pat-original","user_id":"1"}}`,
		`{"balance_query":{"mode":"user_api","access_token":"pat-original","Access_Token":"pat-other","user_id":"1"}}`,
	} {
		t.Run(settings, func(t *testing.T) {
			origin := Channel{Type: constant.ChannelTypeNewAPI, OtherSettings: settings}
			incoming := origin
			incoming.OtherSettings = strings.ReplaceAll(settings, "pat-original", "pat-rotated")

			assert.False(t, incoming.OtherSettingsEqualIgnoringBalanceQueryToken(&origin))
		})
	}
}

func TestRestoreBalanceQueryTokenAfterDuplicateObjectsAreCollapsed(t *testing.T) {
	origin := Channel{Type: constant.ChannelTypeNewAPI,
		OtherSettings: `{"balance_query":{"mode":"user_api","access_token":"pat-first","user_id":"1"},"Balance_Query":{"access_token":"pat-last"}}`,
	}
	incoming := Channel{Type: constant.ChannelTypeNewAPI,
		OtherSettings: `{"balance_query":{"mode":"user_api","user_id":"1"}}`,
	}
	require.NoError(t, origin.ValidateSettings())

	incoming.RestoreBalanceQueryAccessToken(&origin)

	require.NoError(t, incoming.ValidateSettings())
	assert.Equal(t, "pat-last", incoming.GetOtherSettings().BalanceQuery.AccessToken)
}

func TestValidateSettingsStripsAllBalanceQueryCaseVariants(t *testing.T) {
	channel := Channel{Type: constant.ChannelTypeOpenAI,
		OtherSettings: `{"future":{"big":12345678901234567890},"BALANCE_QUERY":{"mode":"user_api","access_token":"pat-upper","user_id":"1"},"balance_query":{"mode":"user_api","ACCESS_TOKEN":"pat-lower","user_id":"1"}}`,
	}

	require.NoError(t, channel.ValidateSettings())

	assert.JSONEq(t, `{"future":{"big":12345678901234567890}}`, channel.OtherSettings)
}

func TestChannelTypeChangePreservesOtherSettingsWhenStrippingBalanceQuery(t *testing.T) {
	for _, test := range []struct {
		name        string
		channelType int
		settings    string
		want        dto.ChannelOtherSettings
	}{
		{
			name:        "case aliases keep the effective speed setting",
			channelType: constant.ChannelTypeAnthropic,
			settings:    `{"allow_speed":false,"ALLOW_SPEED":true,"balance_query":{"mode":"subscription"}}`,
			want:        dto.ChannelOtherSettings{AllowSpeed: true},
		},
		{
			name:        "duplicate objects keep merged custom routes",
			channelType: constant.ChannelTypeAdvancedCustom,
			settings:    `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","upstream_path":"/v1/chat/completions","converter":"none"}]},"advanced_custom":{},"BALANCE_QUERY":{"mode":"subscription"}}`,
			want: dto.ChannelOtherSettings{
				AdvancedCustom: &dto.AdvancedCustomConfig{
					Routes: []dto.AdvancedCustomRoute{{
						IncomingPath: "/v1/chat/completions",
						UpstreamPath: "/v1/chat/completions",
						Converter:    "none",
					}},
				},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			channel := Channel{Type: constant.ChannelTypeNewAPI, OtherSettings: test.settings}
			require.NoError(t, channel.ValidateSettings())

			channel.Type = test.channelType
			require.NoError(t, channel.ValidateSettings())

			assert.Equal(t, test.want, channel.GetOtherSettings())
		})
	}
}

func TestBalanceQueryDuplicateRequestFieldsRemainSensitive(t *testing.T) {
	for _, test := range []struct {
		name     string
		origin   string
		incoming string
	}{
		{
			name:     "reordering URL aliases changes the effective URL",
			origin:   `{"balance_query":{"mode":"custom","url":"https://first.example/balance","URL":"https://last.example/balance","extract":"response.balance"}}`,
			incoming: `{"balance_query":{"mode":"custom","URL":"https://last.example/balance","url":"https://first.example/balance","extract":"response.balance"}}`,
		},
		{
			name:     "an earlier duplicate object changes the effective request",
			origin:   `{"balance_query":{"mode":"custom","url":"https://upstream.example/balance","extract":"response.balance"}}`,
			incoming: `{"balance_query":{"method":"POST","body":"hello"},"balance_query":{"mode":"custom","url":"https://upstream.example/balance","extract":"response.balance"}}`,
		},
		{
			name:     "an earlier duplicate object adds an unknown field",
			origin:   `{"balance_query":{"mode":"custom","url":"https://upstream.example/balance","extract":"response.balance"}}`,
			incoming: `{"balance_query":{"future_field":1},"balance_query":{"mode":"custom","url":"https://upstream.example/balance","extract":"response.balance"}}`,
		},
		{
			name:     "a null alias resets the effective query mode",
			origin:   `{"balance_query":{"mode":"custom","url":"https://upstream.example/balance","extract":"response.balance"},"balance_query":{}}`,
			incoming: `{"balance_query":{"mode":"custom","url":"https://upstream.example/balance","extract":"response.balance"},"Balance_Query":null,"balance_query":{}}`,
		},
		{
			name:     "Unicode aliases use the DTO case folding",
			origin:   `{"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1","u\u017fer_id":"2"}}`,
			incoming: `{"balance_query":{"mode":"user_api","access_token":"pat-test","u\u017fer_id":"2","user_id":"1"}}`,
		},
		{
			name:     "unknown fields cannot impersonate the ordered comparison",
			origin:   `{"__balance_query_ordered__":[{"mode":"custom","url":"https://upstream.example/balance","extract":"response.balance"},{}],"__other__":{}}`,
			incoming: `{"balance_query":{"mode":"custom","url":"https://upstream.example/balance","extract":"response.balance"},"balance_query":{}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			origin := Channel{Type: constant.ChannelTypeNewAPI, OtherSettings: test.origin}
			incoming := Channel{Type: constant.ChannelTypeNewAPI, OtherSettings: test.incoming}
			require.NoError(t, origin.ValidateSettings())
			require.NoError(t, incoming.ValidateSettings())
			assert.False(t, incoming.OtherSettingsEqualIgnoringBalanceQueryToken(&origin))
		})
	}
}

func TestChannelValidateSettingsRejectsInvalidHTTPTransport(t *testing.T) {
	tests := []struct {
		name    string
		setting dto.ChannelSettings
		wantErr string
	}{
		{
			name:    "auto with shards is valid",
			setting: dto.ChannelSettings{HTTPProtocol: "auto", HTTP2ConnectionShards: 4},
		},
		{
			name:    "http1 with shards greater than one rejected",
			setting: dto.ChannelSettings{HTTPProtocol: "http1", HTTP2ConnectionShards: 2},
			wantErr: "http2_connection_shards",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{}
			channel.SetSetting(tt.setting)
			err := channel.ValidateSettings()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestAdvancedCustomChannelRequiresModelListRouteOnlyWhenUpdateChecksEnabled(t *testing.T) {
	inferenceRoute := dto.AdvancedCustomRoute{
		IncomingPath: "/v1/chat/completions",
		UpstreamPath: "/v1/chat/completions",
		Converter:    "none",
	}

	tests := []struct {
		name          string
		checksEnabled bool
		routes        []dto.AdvancedCustomRoute
		wantErr       string
	}{
		{
			name:   "legacy channel without discovery route remains valid",
			routes: []dto.AdvancedCustomRoute{inferenceRoute},
		},
		{
			name:          "enabled checks require discovery route",
			checksEnabled: true,
			routes:        []dto.AdvancedCustomRoute{inferenceRoute},
			wantErr:       dto.AdvancedCustomModelListPath,
		},
		{
			name:          "enabled checks accept discovery route",
			checksEnabled: true,
			routes: []dto.AdvancedCustomRoute{
				inferenceRoute,
				{
					IncomingPath: dto.AdvancedCustomModelListPath,
					UpstreamPath: dto.AdvancedCustomModelListPath,
					Converter:    "none",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{Type: constant.ChannelTypeAdvancedCustom}
			channel.SetOtherSettings(dto.ChannelOtherSettings{
				UpstreamModelUpdateCheckEnabled: tt.checksEnabled,
				AdvancedCustom: &dto.AdvancedCustomConfig{
					Routes: tt.routes,
				},
			})

			err := channel.ValidateSettings()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestNewAPIBalanceQueryRelativeURLRequiresBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		queryURL string
		wantErr  string
	}{
		{
			name:     "relative path with base URL is valid",
			baseURL:  "https://upstream.example",
			queryURL: "/v1/balance",
		},
		{
			name:     "base_url placeholder form with base URL is valid",
			baseURL:  "https://upstream.example",
			queryURL: "{base_url}/v1/balance",
		},
		{
			name:     "relative path without base URL is rejected",
			baseURL:  "",
			queryURL: "/v1/balance",
			wantErr:  "channel base URL is required when the balance query URL is relative",
		},
		{
			name:     "absolute URL without base URL stays valid",
			baseURL:  "",
			queryURL: "https://api.example.com/balance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{Type: constant.ChannelTypeNewAPI}
			if tt.baseURL != "" {
				channel.BaseURL = &tt.baseURL
			}
			channel.SetOtherSettings(dto.ChannelOtherSettings{
				BalanceQuery: &dto.ChannelBalanceQuery{
					Mode:    dto.BalanceQueryModeCustom,
					URL:     tt.queryURL,
					Extract: "response.balance",
				},
			})

			err := channel.ValidateSettings()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidateSettingsStripsBalanceQueryForNonNewAPIChannels(t *testing.T) {
	// Balance query settings only apply to New API channels. A channel whose
	// type changed away from New API must not retain the credential-bearing
	// block on save, and keys unknown to the settings DTO must survive the
	// strip byte-identically.
	channel := &Channel{Type: constant.ChannelTypeOpenAI}
	channel.OtherSettings = `{"future_field":{"big":12345678901234567890},"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"}}`
	require.NoError(t, channel.ValidateSettings())
	assert.NotContains(t, channel.OtherSettings, "balance_query")
	assert.NotContains(t, channel.OtherSettings, "pat-secret")
	var raw map[string]json.RawMessage
	require.NoError(t, common.Unmarshal([]byte(channel.OtherSettings), &raw))
	assert.JSONEq(t, `{"big":12345678901234567890}`, string(raw["future_field"]))

	// New API channels keep their balance query settings.
	newAPIChannel := &Channel{Type: constant.ChannelTypeNewAPI}
	newAPIChannel.OtherSettings = `{"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"}}`
	require.NoError(t, newAPIChannel.ValidateSettings())
	assert.Contains(t, newAPIChannel.OtherSettings, "access_token")

	// Idempotent: settings without balance_query are left untouched.
	plain := &Channel{Type: constant.ChannelTypeOpenAI}
	plain.OtherSettings = `{"openrouter_enterprise":false}`
	require.NoError(t, plain.ValidateSettings())
	assert.JSONEq(t, `{"openrouter_enterprise":false}`, plain.OtherSettings)
}

func TestCanonicalizeOtherSettingsForCompare(t *testing.T) {
	tests := []struct {
		name           string
		raw            string
		want           string
		wantToken      string
		wantUnparsable bool
	}{
		{
			name: "bare subscription default is absent",
			raw:  `{"balance_query":{"mode":"subscription"}}`,
			want: `{}`,
		},
		{
			name: "empty mode counts as the subscription default",
			raw:  `{"balance_query":{"mode":""}}`,
			want: `{}`,
		},
		{
			name: "default is dropped while other keys survive byte-exactly",
			raw:  `{"a":1,"balance_query":{"mode":"subscription"},"future":{"big":12345678901234567890}}`,
			want: `{"a":1,"future":{"big":12345678901234567890}}`,
		},
		{
			name:      "user_api token is extracted and excised",
			raw:       `{"balance_query":{"mode":"user_api","access_token":"pat","user_id":"1"}}`,
			want:      `{"balance_query":{"mode":"user_api","user_id":"1"}}`,
			wantToken: "pat",
		},
		{
			name:      "non-string token is compared by raw bytes",
			raw:       `{"balance_query":{"access_token":123,"mode":"user_api"}}`,
			want:      `{"balance_query":{"mode":"user_api"}}`,
			wantToken: "123",
		},
		{
			name: "unreadable balance_query is dropped",
			raw:  `{"a":1,"balance_query":[1,2]}`,
			want: `{"a":1}`,
		},
		{
			name: "empty settings canonicalize to an empty object",
			raw:  "",
			want: `{}`,
		},
		{
			name:           "unparseable settings report not-ok",
			raw:            `{`,
			wantUnparsable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{OtherSettings: tt.raw}
			canonical, token, ok := channel.canonicalizeOtherSettingsForCompare()
			if tt.wantUnparsable {
				assert.False(t, ok)
				return
			}
			require.True(t, ok)
			assert.JSONEq(t, tt.want, canonical)
			assert.Equal(t, tt.wantToken, token)
		})
	}

	// Byte fidelity: two distinct big integers must not collapse into an
	// equal comparison through a float64 round-trip.
	left := &Channel{OtherSettings: `{"n":100000000000000000000}`}
	right := &Channel{OtherSettings: `{"n":100000000000000000001}`}
	leftCanonical, _, leftOK := left.canonicalizeOtherSettingsForCompare()
	rightCanonical, _, rightOK := right.canonicalizeOtherSettingsForCompare()
	require.True(t, leftOK)
	require.True(t, rightOK)
	assert.NotEqual(t, leftCanonical, rightCanonical)
}

func TestOtherSettingsEqualIgnoringBalanceQueryTokenKeySet(t *testing.T) {
	origin := &Channel{Type: constant.ChannelTypeNewAPI}
	origin.OtherSettings = `{"openrouter_enterprise":false,"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"}}`

	tests := []struct {
		name     string
		incoming string
		want     bool
	}{
		{
			name:     "adding an unknown top-level field is a change",
			incoming: `{"openrouter_enterprise":false,"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"},"new_field":1}`,
			want:     false,
		},
		{
			name:     "removing an existing top-level field is a change",
			incoming: `{"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"}}`,
			want:     false,
		},
		{
			name:     "changing an unknown top-level field is a change",
			incoming: `{"openrouter_enterprise":true,"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"}}`,
			want:     false,
		},
		{
			name:     "same keys same values stays equal",
			incoming: `{"openrouter_enterprise":false,"balance_query":{"mode":"user_api","access_token":"pat-secret","user_id":"1"}}`,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			incoming := &Channel{Type: constant.ChannelTypeNewAPI}
			incoming.OtherSettings = tt.incoming
			assert.Equal(t, tt.want, incoming.OtherSettingsEqualIgnoringBalanceQueryToken(origin))
		})
	}
}

func TestOtherSettingsEqualIgnoringBalanceQueryTokenUnparseable(t *testing.T) {
	// Unparseable settings must fail closed: a normal edit against a channel
	// with corrupt stored settings is a sensitive change (same as the legacy
	// byte-inequality behavior), not a free pass and not a hard error for the
	// root path.
	origin := &Channel{Type: constant.ChannelTypeNewAPI}
	origin.OtherSettings = `{`

	incoming := &Channel{Type: constant.ChannelTypeNewAPI}
	incoming.OtherSettings = `{"openrouter_enterprise":false}`
	assert.False(t, incoming.OtherSettingsEqualIgnoringBalanceQueryToken(origin),
		"unparseable origin settings must not compare equal")

	// A redacted round-trip that canonicalizes to the same content the
	// redaction would emit is still equal: redaction drops the unreadable
	// balance_query entirely.
	roundTrip := &Channel{Type: constant.ChannelTypeNewAPI}
	roundTrip.OtherSettings = `{}`
	assert.True(t, roundTrip.OtherSettingsEqualIgnoringBalanceQueryToken(
		&Channel{Type: constant.ChannelTypeNewAPI, OtherSettings: `{"balance_query":"corrupt"}`}))
}

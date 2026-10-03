package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManualBalanceRefreshDoesNotExposeRedirectCredentials(t *testing.T) {
	for _, test := range []struct {
		name         string
		query        *dto.ChannelBalanceQuery
		secrets      []string
		redirectPath string
	}{
		{
			name:         "subscription channel key",
			query:        &dto.ChannelBalanceQuery{Mode: dto.BalanceQueryModeSubscription},
			secrets:      []string{"sk-channel-key"},
			redirectPath: "/v1/dashboard/billing/subscription",
		},
		{
			name:         "subscription usage channel key",
			query:        &dto.ChannelBalanceQuery{Mode: dto.BalanceQueryModeSubscription},
			secrets:      []string{"sk-channel-key"},
			redirectPath: "/v1/dashboard/billing/usage",
		},
		{
			name: "user_api access token",
			query: &dto.ChannelBalanceQuery{
				Mode: dto.BalanceQueryModeUserAPI, AccessToken: "pat-user-secret", UserId: "1",
			},
			secrets: []string{"pat-user-secret"},
		},
		{
			name: "custom request credentials",
			query: &dto.ChannelBalanceQuery{
				Mode: dto.BalanceQueryModeCustom, Method: http.MethodPost, URL: "/balance",
				Headers: map[string]string{
					"Authorization": "Bearer pat-header-secret", "X-API-Key": "pat-extra-secret",
				},
				Body: "pat-body-secret", Extract: "response.balance",
			},
			secrets: []string{"pat-header-secret", "pat-extra-secret", "pat-body-secret"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := setupBalanceRegressionDB(t, common.DatabaseTypeSQLite)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.redirectPath != "" && r.URL.Path != test.redirectPath {
					_, _ = w.Write([]byte(`{"hard_limit_usd":10}`))
					return
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				credentials := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") +
					":" + r.Header.Get("X-API-Key") + ":" + string(body)
				// net/http includes the raw Location in this parsing error.
				w.Header().Set("Location", "https://upstream.example/%ZZ?echo="+url.QueryEscape(credentials))
				w.WriteHeader(http.StatusFound)
			}))
			defer server.Close()
			channel := model.Channel{
				Type: constant.ChannelTypeNewAPI, Key: "sk-channel-key", BaseURL: &server.URL,
				Balance: 7, BalanceUpdatedTime: 123,
			}
			channel.SetOtherSettings(dto.ChannelOtherSettings{BalanceQuery: test.query})
			require.NoError(t, db.Create(&channel).Error)
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
			if test.query.NormalizedMode() != dto.BalanceQueryModeSubscription {
				assert.Contains(t, response.Message, test.query.Mode+" balance query failed")
			}
			for _, secret := range test.secrets {
				assert.NotContains(t, response.Message, secret)
			}
			var stored model.Channel
			require.NoError(t, db.First(&stored, channel.Id).Error)
			assert.Equal(t, 7.0, stored.Balance)
			assert.Equal(t, int64(123), stored.BalanceUpdatedTime)
		})
	}
}

func TestSubscriptionBalanceRefreshClearsFailureAfterModeSwitch(t *testing.T) {
	previousInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = previousInterval })
	for _, databaseType := range []common.DatabaseType{
		common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL,
	} {
		t.Run(string(databaseType), func(t *testing.T) {
			db := setupBalanceRegressionDB(t, databaseType)
			require.NoError(t, db.AutoMigrate(&model.Ability{}))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/dashboard/billing/subscription" {
					_, _ = w.Write([]byte(`{"hard_limit_usd":10}`))
					return
				}
				_, _ = w.Write([]byte(`{"total_usage":0}`))
			}))
			defer server.Close()
			for _, test := range []struct {
				name     string
				settings string
			}{
				{"explicit subscription", `{"balance_query":{"mode":"subscription"}}`},
			} {
				for _, refresh := range []string{"manual", "automatic"} {
					t.Run(test.name+"/"+refresh, func(t *testing.T) {
						channel := model.Channel{
							Type: constant.ChannelTypeNewAPI, Name: "mode switch", Key: "sk-test", BaseURL: &server.URL,
							Status: common.ChannelStatusEnabled, Models: "gpt-test", Group: "default",
							OtherSettings: `{"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1"}}`,
							ChannelInfo: model.ChannelInfo{
								MultiKeyMode: constant.MultiKeyModePolling, BalanceQueryLastFailedTime: 123,
							},
						}
						require.NoError(t, db.Create(&channel).Error)
						t.Cleanup(func() {
							require.NoError(t, db.Where("channel_id = ?", channel.Id).Delete(&model.Ability{}).Error)
							require.NoError(t, db.Delete(&model.Channel{}, channel.Id).Error)
						})
						require.NoError(t, channel.UpdatePreservingBalanceQueryToken(test.settings, true))

						if refresh == "automatic" {
							require.NoError(t, updateAllChannelsBalance())
						} else {
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
							require.True(t, response.Success, response.Message)
						}

						var stored model.Channel
						require.NoError(t, db.First(&stored, channel.Id).Error)
						assert.Equal(t, 10.0, stored.Balance)
						assert.Zero(t, stored.ChannelInfo.BalanceQueryLastFailedTime)
						assert.Equal(t, constant.MultiKeyModePolling, stored.ChannelInfo.MultiKeyMode)
					})
				}
			}
		})
	}
}

func TestBalanceRequestRetainsTimeoutDiagnosis(t *testing.T) {
	previousTimeout := balanceQueryRequestTimeout
	balanceQueryRequestTimeout = 50 * time.Millisecond
	t.Cleanup(func() { balanceQueryRequestTimeout = previousTimeout })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	channel := &model.Channel{Type: constant.ChannelTypeNewAPI, BaseURL: &server.URL}
	query := &dto.ChannelBalanceQuery{
		Mode: dto.BalanceQueryModeUserAPI, AccessToken: "pat-user-secret", UserId: "1",
	}

	_, err := fetchNewAPIUserAPIBalance(channel, query, "sk-channel-key")

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NotContains(t, err.Error(), query.AccessToken)
	assert.NotContains(t, err.Error(), server.URL)
}

func TestSubscriptionBalanceRejectsInvalidUpstreamResponses(t *testing.T) {
	previousInterval, previousRedis := common.RequestInterval, common.RedisEnabled
	common.RequestInterval, common.RedisEnabled = 0, false
	t.Cleanup(func() { common.RequestInterval, common.RedisEnabled = previousInterval, previousRedis })
	for _, databaseType := range []common.DatabaseType{
		common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL,
	} {
		t.Run(string(databaseType), func(t *testing.T) {
			db := setupBalanceRegressionDB(t, databaseType)
			require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.User{}))
			for _, test := range []struct {
				name         string
				subscription string
				usage        string
				valid        bool
				balance      float64
			}{
				{"subscription error", `{"error":{"message":"temporary failure"}}`, `{"total_usage":0}`, false, 0},
				{"missing limit", `{}`, `{"total_usage":0}`, false, 0},
				{"null limit", `{"hard_limit_usd":null}`, `{"total_usage":0}`, false, 0},
				{"usage error", `{"hard_limit_usd":10}`, `{"error":{"message":"temporary failure"}}`, false, 0},
				{"missing usage", `{"hard_limit_usd":10}`, `{}`, false, 0},
				{"null usage", `{"hard_limit_usd":10}`, `{"total_usage":null}`, false, 0},
				{"valid balance", `{"hard_limit_usd":10}`, `{"total_usage":200}`, true, 8},
				{"explicit zero", `{"hard_limit_usd":0}`, `{"total_usage":0}`, true, 0},
			} {
				t.Run(test.name, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == "/v1/dashboard/billing/subscription" {
							_, _ = w.Write([]byte(test.subscription))
							return
						}
						_, _ = w.Write([]byte(test.usage))
					}))
					defer server.Close()
					autoBan := 1
					channel := model.Channel{
						Type: constant.ChannelTypeNewAPI, Name: "subscription upstream", Key: "sk-test", OtherSettings: `{"balance_query":{"mode":"subscription"}}`,
						BaseURL: &server.URL, Status: common.ChannelStatusEnabled, AutoBan: &autoBan,
						Balance: 7, BalanceUpdatedTime: 123,
					}
					require.NoError(t, db.Create(&channel).Error)
					t.Cleanup(func() { require.NoError(t, db.Delete(&model.Channel{}, channel.Id).Error) })

					balance, err := updateStandardChannelBalance(&channel)
					if test.valid {
						require.NoError(t, err)
						assert.Equal(t, test.balance, balance)
					} else {
						assert.Error(t, err)
					}
					require.NoError(t, updateAllChannelsBalance())
					var stored model.Channel
					require.NoError(t, db.First(&stored, channel.Id).Error)
					if !test.valid {
						assert.Equal(t, 7.0, stored.Balance)
						assert.Equal(t, int64(123), stored.BalanceUpdatedTime)
						assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
						return
					}
					assert.Equal(t, test.balance, stored.Balance)
					if test.balance == 0 {
						assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
					} else {
						assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
					}
				})
			}
		})
	}
}

func TestChannelEditPreservesLatestBalanceQueryFailure(t *testing.T) {
	for _, databaseType := range []common.DatabaseType{
		common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL,
	} {
		t.Run(string(databaseType), func(t *testing.T) {
			db := setupBalanceRegressionDB(t, databaseType)
			require.NoError(t, db.AutoMigrate(&model.Ability{}))
			for _, test := range []struct {
				name    string
				initial int64
				latest  int64
			}{
				{"refresh marks failure after editor reads", 0, 456},
				{"refresh clears failure after editor reads", 123, 0},
			} {
				t.Run(test.name, func(t *testing.T) {
					baseURL := "https://upstream.example"
					channel := model.Channel{
						Type: constant.ChannelTypeNewAPI, Name: "before", Key: "sk-test", BaseURL: &baseURL,
						Models: "gpt-test", Group: "default",
						ChannelInfo: model.ChannelInfo{MultiKeyMode: constant.MultiKeyModeRandom, BalanceQueryLastFailedTime: test.initial},
					}
					require.NoError(t, db.Create(&channel).Error)
					t.Cleanup(func() {
						require.NoError(t, db.Where("channel_id = ?", channel.Id).Delete(&model.Ability{}).Error)
						require.NoError(t, db.Delete(&model.Channel{}, channel.Id).Error)
					})
					editor := model.Channel{Id: channel.Id, Name: "after", ChannelInfo: channel.ChannelInfo}
					if test.latest == 0 {
						require.NoError(t, channel.ClearBalanceQueryFailure())
					} else {
						require.NoError(t, channel.MarkBalanceQueryFailure(test.latest))
					}

					require.NoError(t, editor.UpdatePreservingBalanceQueryToken("", false))

					var stored model.Channel
					require.NoError(t, db.First(&stored, channel.Id).Error)
					assert.Equal(t, "after", stored.Name)
					assert.Equal(t, test.latest, stored.ChannelInfo.BalanceQueryLastFailedTime)
					assert.Equal(t, test.latest, editor.ChannelInfo.BalanceQueryLastFailedTime)
				})
			}
		})
	}
}

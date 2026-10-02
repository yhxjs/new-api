package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupBalanceRegressionDB(t *testing.T, databaseType common.DatabaseType) *gorm.DB {
	t.Helper()
	previousGinMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousGinMode) })
	var dialector gorm.Dialector
	switch databaseType {
	case common.DatabaseTypeMySQL:
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		dialector = mysql.Open(dsn)
	case common.DatabaseTypePostgreSQL:
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		dialector = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
	default:
		dialector = sqlite.Open(filepath.Join(t.TempDir(), "balance.db"))
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	versionQuery := "SELECT version()"
	if databaseType == common.DatabaseTypeSQLite {
		versionQuery = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("database version: %s", version)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	previousDB, previousType := model.DB, common.MainDatabaseType()
	previousMemoryCache := common.MemoryCacheEnabled
	model.DB = db
	common.SetMainDatabaseType(databaseType)
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
		common.MemoryCacheEnabled = previousMemoryCache
	})
	return db
}

func TestRejectedChannelUpdateDoesNotPersistMalformedSettings(t *testing.T) {
	for _, databaseType := range []common.DatabaseType{
		common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL,
	} {
		t.Run(string(databaseType), func(t *testing.T) {
			db := setupBalanceRegressionDB(t, databaseType)
			for _, settings := range []string{"{", `{"balance_query":{"access_token":123}}`} {
				t.Run(settings, func(t *testing.T) {
					baseURL := "https://upstream.example"
					original := model.Channel{
						Type: constant.ChannelTypeNewAPI, Name: "original", Key: "sk-original",
						Status: common.ChannelStatusEnabled, Models: "gpt-test", Group: "default",
						BaseURL: &baseURL, Balance: 50, OtherSettings: "{}",
					}
					require.NoError(t, db.Create(&original).Error)
					t.Cleanup(func() { require.NoError(t, db.Delete(&model.Channel{}, original.Id).Error) })
					body, err := common.Marshal(map[string]any{
						"id": original.Id, "type": original.Type, "key": "sk-unauthorized", "settings": settings,
					})
					require.NoError(t, err)
					recorder := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(recorder)
					ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", bytes.NewReader(body))
					ctx.Request.Header.Set("Content-Type", "application/json")

					UpdateChannel(ctx)

					var response struct {
						Success bool `json:"success"`
					}
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
					require.False(t, response.Success)
					var stored model.Channel
					require.NoError(t, db.First(&stored, original.Id).Error)
					assert.Equal(t, original, stored, "a rejected update must leave every stored field unchanged")
				})
			}
		})
	}
}

func TestUpdateChannelWithoutTypePreservesBalanceQuery(t *testing.T) {
	for _, databaseType := range []common.DatabaseType{
		common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL,
	} {
		t.Run(string(databaseType), func(t *testing.T) {
			db := setupBalanceRegressionDB(t, databaseType)
			require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.User{}, &model.Log{}))
			previousLogDB, previousRedisEnabled := model.LOG_DB, common.RedisEnabled
			previousLogDatabaseType := common.LogDatabaseType()
			model.LOG_DB, common.RedisEnabled = db, false
			common.SetLogDatabaseType(databaseType)
			t.Cleanup(func() {
				model.LOG_DB, common.RedisEnabled = previousLogDB, previousRedisEnabled
				common.SetLogDatabaseType(previousLogDatabaseType)
			})
			operator := model.User{Username: "balance-update-root", Role: common.RoleRootUser}
			require.NoError(t, db.Create(&operator).Error)
			t.Cleanup(func() {
				require.NoError(t, db.Where("user_id = ?", operator.Id).Delete(&model.Log{}).Error)
				require.NoError(t, db.Unscoped().Delete(&model.User{}, operator.Id).Error)
			})
			for _, test := range []struct {
				name         string
				settings     string
				omitSettings bool
			}{
				{"redacted user API settings", `{"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1"}}`, false},
				{"relative custom settings", `{"balance_query":{"mode":"custom","url":"/balance","extract":"response.balance"}}`, false},
				{"uppercase query settings", `{"BALANCE_QUERY":{"mode":"user_api","ACCESS_TOKEN":"pat-test","user_id":"1"}}`, false},
				{"duplicate query settings", `{"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1"},"balance_query":{"quota_per_unit":500000}}`, false},
				{"null-reset query settings", `{"balance_query":{"quota_per_unit":1000},"Balance_Query":null,"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1"}}`, false},
				{"omitted settings", `{"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1"}}`, true},
			} {
				t.Run(test.name, func(t *testing.T) {
					baseURL := "https://upstream.example"
					original := model.Channel{
						Type: constant.ChannelTypeNewAPI, Name: "before", Key: "sk-test",
						BaseURL: &baseURL, Models: "gpt-test", Group: "default", OtherSettings: test.settings,
					}
					require.NoError(t, db.Create(&original).Error)
					t.Cleanup(func() {
						require.NoError(t, db.Where("channel_id = ?", original.Id).Delete(&model.Ability{}).Error)
						require.NoError(t, db.Delete(&model.Channel{}, original.Id).Error)
					})
					payload := map[string]any{"id": original.Id, "name": "after"}
					if !test.omitSettings {
						redacted := original
						redacted.RedactBalanceQueryAccessToken()
						payload["settings"] = redacted.OtherSettings
					}
					body, err := common.Marshal(payload)
					require.NoError(t, err)
					recorder := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(recorder)
					ctx.Set("id", operator.Id)
					ctx.Set("role", common.RoleRootUser)
					ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", bytes.NewReader(body))
					ctx.Request.Header.Set("Content-Type", "application/json")

					UpdateChannel(ctx)

					var response struct {
						Success bool   `json:"success"`
						Message string `json:"message"`
					}
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
					require.True(t, response.Success, response.Message)
					var stored model.Channel
					require.NoError(t, db.First(&stored, original.Id).Error)
					assert.Equal(t, "after", stored.Name)
					assert.Equal(t, original.Type, stored.Type)
					assert.Equal(t, original.Key, stored.Key)
					assert.Equal(t, original.BaseURL, stored.BaseURL)
					assert.JSONEq(t, original.OtherSettings, stored.OtherSettings)
					assert.Equal(t, original.GetOtherSettings().BalanceQuery, stored.GetOtherSettings().BalanceQuery)
					assert.NotContains(t, recorder.Body.String(), "pat-test")
				})
			}
		})
	}
}

func TestUpdateChannelExplicitEmptySettingsClearsStoredSettings(t *testing.T) {
	db := setupBalanceRegressionDB(t, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.User{}, &model.Log{}))
	previousLogDB, previousRedisEnabled := model.LOG_DB, common.RedisEnabled
	previousLogDatabaseType := common.LogDatabaseType()
	model.LOG_DB, common.RedisEnabled = db, false
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.LOG_DB, common.RedisEnabled = previousLogDB, previousRedisEnabled
		common.SetLogDatabaseType(previousLogDatabaseType)
	})
	operator := model.User{Username: "clear-settings-root", Role: common.RoleRootUser}
	require.NoError(t, db.Create(&operator).Error)
	t.Cleanup(func() {
		require.NoError(t, db.Where("user_id = ?", operator.Id).Delete(&model.Log{}).Error)
		require.NoError(t, db.Unscoped().Delete(&model.User{}, operator.Id).Error)
	})
	baseURL := "https://upstream.example"
	channel := model.Channel{
		Type: constant.ChannelTypeNewAPI, Name: "clear settings", Key: "sk-test", BaseURL: &baseURL,
		OtherSettings: `{"balance_query":{"mode":"user_api","access_token":"pat-old","user_id":"1"}}`,
	}
	require.NoError(t, db.Create(&channel).Error)
	t.Cleanup(func() { require.NoError(t, db.Delete(&model.Channel{}, channel.Id).Error) })

	body, err := common.Marshal(map[string]any{
		"id": channel.Id, "type": channel.Type, "base_url": baseURL, "settings": "",
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", operator.Id)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateChannel(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	assert.NotContains(t, recorder.Body.String(), "pat-old")
	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Empty(t, stored.OtherSettings)
}

func TestManualBalanceRefreshClearsFailureStamp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"quota":250000}}`))
	}))
	defer server.Close()
	for _, databaseType := range []common.DatabaseType{
		common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL,
	} {
		t.Run(string(databaseType), func(t *testing.T) {
			db := setupBalanceRegressionDB(t, databaseType)
			channel := model.Channel{
				Type: constant.ChannelTypeNewAPI, Key: "sk-test", BaseURL: &server.URL,
				OtherSettings: `{"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1"}}`,
				ChannelInfo:   model.ChannelInfo{BalanceQueryLastFailedTime: 1, MultiKeyMode: constant.MultiKeyMode("polling")},
			}
			require.NoError(t, db.Create(&channel).Error)
			t.Cleanup(func() { require.NoError(t, db.Delete(&model.Channel{}, channel.Id).Error) })
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channel.Id)}}
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/update_balance/"+strconv.Itoa(channel.Id), nil)

			UpdateChannelBalance(ctx)

			var response struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)
			var stored model.Channel
			require.NoError(t, db.First(&stored, channel.Id).Error)
			assert.Equal(t, 0.5, stored.Balance)
			assert.Zero(t, stored.ChannelInfo.BalanceQueryLastFailedTime)
			assert.Equal(t, channel.ChannelInfo.MultiKeyMode, stored.ChannelInfo.MultiKeyMode)
		})
	}
}

func TestManualNewAPIBalanceRefreshValidatesQuotaPresence(t *testing.T) {
	for _, databaseType := range []common.DatabaseType{
		common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL,
	} {
		t.Run(string(databaseType), func(t *testing.T) {
			db := setupBalanceRegressionDB(t, databaseType)
			for _, test := range []struct {
				name    string
				body    string
				success bool
			}{
				{"missing quota preserves the previous balance", `{"success":true,"data":{}}`, false},
				{"null quota preserves the previous balance", `{"success":true,"data":{"quota":null}}`, false},
				{"explicit zero quota updates the balance", `{"success":true,"data":{"quota":0}}`, true},
			} {
				t.Run(test.name, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						_, _ = w.Write([]byte(test.body))
					}))
					defer server.Close()
					channel := model.Channel{
						Type: constant.ChannelTypeNewAPI, Key: "sk-test", BaseURL: &server.URL,
						Balance: 7, BalanceUpdatedTime: 123,
						OtherSettings: `{"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1"}}`,
						ChannelInfo:   model.ChannelInfo{BalanceQueryLastFailedTime: 456},
					}
					require.NoError(t, db.Create(&channel).Error)
					t.Cleanup(func() { require.NoError(t, db.Delete(&model.Channel{}, channel.Id).Error) })
					recorder := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(recorder)
					ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channel.Id)}}
					ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/update_balance/"+strconv.Itoa(channel.Id), nil)

					UpdateChannelBalance(ctx)

					var response struct {
						Success bool `json:"success"`
					}
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
					assert.Equal(t, test.success, response.Success)
					var stored model.Channel
					require.NoError(t, db.First(&stored, channel.Id).Error)
					if test.success {
						assert.Zero(t, stored.Balance)
						assert.NotEqual(t, channel.BalanceUpdatedTime, stored.BalanceUpdatedTime)
						assert.Zero(t, stored.ChannelInfo.BalanceQueryLastFailedTime)
						return
					}
					assert.Equal(t, channel.Balance, stored.Balance)
					assert.Equal(t, channel.BalanceUpdatedTime, stored.BalanceUpdatedTime)
					assert.Equal(t, channel.ChannelInfo, stored.ChannelInfo)
				})
			}
		})
	}
}

func TestManualCustomBalanceRefreshRedactsExpressionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"balance":"invalid credentials sk-channel-key pat-header-secret pat-body-secret"}`))
	}))
	defer server.Close()
	for _, databaseType := range []common.DatabaseType{
		common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL,
	} {
		t.Run(string(databaseType), func(t *testing.T) {
			db := setupBalanceRegressionDB(t, databaseType)
			channel := model.Channel{
				Type: constant.ChannelTypeNewAPI, Key: "sk-channel-key", BaseURL: &server.URL,
				OtherSettings: `{"balance_query":{"mode":"custom","method":"POST","url":"/balance","headers":{"Authorization":"Bearer pat-header-secret"},"body":"pat-body-secret","extract":"float(json(\"balance\"))"}}`,
			}
			require.NoError(t, db.Create(&channel).Error)
			t.Cleanup(func() { require.NoError(t, db.Delete(&model.Channel{}, channel.Id).Error) })
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
			assert.Contains(t, response.Message, "balance extract expression run error")
			assert.NotContains(t, response.Message, channel.Key)
			assert.NotContains(t, response.Message, "pat-header-secret")
			assert.NotContains(t, response.Message, "pat-body-secret")
		})
	}
}

func TestBalanceExtractRejectsUnboundedExpressions(t *testing.T) {
	for _, extract := range []string{
		`sum(map(response.items, {let x = #; count(response.items, {# == x})}))`,
		`1 in 1..3 ? 1 : 0`,
		`len(repeat("1", 20))`,
		`let x = response.value; let y = x + x; float(y)`,
		`json('@pretty:{"indent":"    "}|@tostr') != '' ? 1 : 0`,
		`$env.json('@pretty:{"indent":"    "}|@tostr') != '' ? 1 : 0`,
		`json(response.path) != nil ? 1 : 0`,
		`json("[data.quota,data.quota]") != nil ? 1 : 0`,
	} {
		t.Run(extract, func(t *testing.T) {
			_, err := runBalanceExtractExpr(extract, []byte(`{"items":[1,1,1],"value":"1","path":"value"}`))
			require.Error(t, err)
			channel := model.Channel{Type: constant.ChannelTypeNewAPI,
				OtherSettings: `{"balance_query":{"mode":"custom","url":"https://upstream.example/balance","extract":` + strconv.Quote(extract) + `}}`,
			}
			require.Error(t, channel.ValidateSettings(), "unsafe expressions must also be rejected when saving")
		})
	}
}

func TestAutomaticBalanceRefreshClearsConcurrentFailureStamp(t *testing.T) {
	previousInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = previousInterval })
	for _, databaseType := range []common.DatabaseType{
		common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL,
	} {
		t.Run(string(databaseType), func(t *testing.T) {
			db := setupBalanceRegressionDB(t, databaseType)
			channel := model.Channel{
				Type: constant.ChannelTypeNewAPI, Key: "sk-test", Status: common.ChannelStatusEnabled,
				OtherSettings: `{"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1"}}`,
			}
			writeResult := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Another refresh fails after this batch has loaded its snapshot
				// but before this successful response reaches the updater.
				writeResult <- db.Model(&channel).Update("channel_info",
					model.ChannelInfo{BalanceQueryLastFailedTime: 1}).Error
				_, _ = w.Write([]byte(`{"success":true,"data":{"quota":250000}}`))
			}))
			defer server.Close()
			channel.BaseURL = &server.URL
			require.NoError(t, db.Create(&channel).Error)
			t.Cleanup(func() { require.NoError(t, db.Delete(&model.Channel{}, channel.Id).Error) })

			require.NoError(t, updateAllChannelsBalance())

			require.NoError(t, <-writeResult)
			var stored model.Channel
			require.NoError(t, db.First(&stored, channel.Id).Error)
			assert.Equal(t, 0.5, stored.Balance)
			assert.Zero(t, stored.ChannelInfo.BalanceQueryLastFailedTime)
		})
	}
}

func TestAutomaticBalanceRefreshFailurePreservesConcurrentChannelInfo(t *testing.T) {
	previousInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = previousInterval })
	db := setupBalanceRegressionDB(t, common.DatabaseTypeSQLite)
	channel := model.Channel{
		Type: constant.ChannelTypeNewAPI, Key: "sk-test", Status: common.ChannelStatusEnabled,
		OtherSettings: `{"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1"}}`,
	}
	writeResult := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		concurrentInfo := model.ChannelInfo{
			IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModePolling,
		}
		writeResult <- db.Model(&model.Channel{}).Where("id = ?", channel.Id).
			Update("channel_info", concurrentInfo).Error
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	channel.BaseURL = &server.URL
	require.NoError(t, db.Create(&channel).Error)
	t.Cleanup(func() { require.NoError(t, db.Delete(&model.Channel{}, channel.Id).Error) })

	require.NoError(t, updateAllChannelsBalance())
	require.NoError(t, <-writeResult)
	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.True(t, stored.ChannelInfo.IsMultiKey)
	assert.Equal(t, 2, stored.ChannelInfo.MultiKeySize)
	assert.Equal(t, constant.MultiKeyModePolling, stored.ChannelInfo.MultiKeyMode)
	assert.Positive(t, stored.ChannelInfo.BalanceQueryLastFailedTime)
}

func TestUpstreamModelSettingsPreserveEffectiveBalanceQuery(t *testing.T) {
	for _, databaseType := range []common.DatabaseType{
		common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL,
	} {
		t.Run(string(databaseType), func(t *testing.T) {
			db := setupBalanceRegressionDB(t, databaseType)
			for _, test := range []struct {
				name     string
				settings string
			}{
				{
					name:     "case aliases keep the last token",
					settings: `{"balance_query":{"mode":"user_api","access_token":"pat-first","user_id":"1"},"Balance_Query":{"ACCESS_TOKEN":"pat-last"}}`,
				},
				{
					name:     "repeated objects keep merged credentials and quota ratio",
					settings: `{"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1"},"balance_query":{"quota_per_unit":1000}}`,
				},
				{
					name:     "a null alias keeps the query disabled",
					settings: `{"balance_query":{"mode":"user_api","access_token":"pat-test","user_id":"1"},"Balance_Query":null}`,
				},
				{
					name:     "null settings accept discovery results",
					settings: `null`,
				},
			} {
				t.Run(test.name, func(t *testing.T) {
					channel := model.Channel{
						Type: constant.ChannelTypeNewAPI, Key: "sk-test", Models: "gpt-test",
						OtherSettings: test.settings,
					}
					require.NoError(t, channel.ValidateSettings())
					require.NoError(t, db.Create(&channel).Error)
					t.Cleanup(func() { require.NoError(t, db.Delete(&model.Channel{}, channel.Id).Error) })
					settings := channel.GetOtherSettings()
					settings.UpstreamModelUpdateLastCheckTime = 123
					settings.UpstreamModelUpdateLastDetectedModels = []string{"gpt-next"}

					require.NoError(t, channel.UpdateUpstreamModelSettings(settings, false))

					var stored model.Channel
					require.NoError(t, db.First(&stored, channel.Id).Error)
					require.NoError(t, stored.ValidateSettings())
					assert.Equal(t, settings, stored.GetOtherSettings())
					assert.Equal(t, channel.Models, stored.Models)
				})
			}
		})
	}
}

func TestUpstreamModelSettingsReplaceDiscoveryAliasesAndPreserveOtherSettings(t *testing.T) {
	for _, databaseType := range []common.DatabaseType{
		common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL,
	} {
		t.Run(string(databaseType), func(t *testing.T) {
			db := setupBalanceRegressionDB(t, databaseType)
			channel := model.Channel{
				Type: constant.ChannelTypeNewAPI, Key: "sk-test", Models: "gpt-test",
				OtherSettings: `{"UPSTREAM_MODEL_UPDATE_CHECK_ENABLED":true,"allow_service_tier":false,"ALLOW_SERVICE_TIER":true,"future":{"quota":12345678901234567890}}`,
			}
			require.NoError(t, db.Create(&channel).Error)
			t.Cleanup(func() { require.NoError(t, db.Delete(&model.Channel{}, channel.Id).Error) })
			settings := channel.GetOtherSettings()
			settings.UpstreamModelUpdateCheckEnabled = false
			settings.UpstreamModelUpdateLastCheckTime = 123
			channel.Models = "gpt-test,gpt-next"

			require.NoError(t, channel.UpdateUpstreamModelSettings(settings, true))

			var stored model.Channel
			require.NoError(t, db.First(&stored, channel.Id).Error)
			assert.Equal(t, settings, stored.GetOtherSettings())
			assert.Equal(t, "gpt-test,gpt-next", stored.Models)
			assert.Contains(t, stored.OtherSettings, `"future":{"quota":12345678901234567890}`)
		})
	}
}

func TestBalanceExtractSupportsScalarExpressions(t *testing.T) {
	for _, test := range []struct {
		extract string
		want    float64
	}{
		{`response.data.quota / 500000`, 0.5},
		{`json("data.quota") / 500000`, 0.5},
		{`max(0, min(ceil(abs(-0.5)), floor(2.9)))`, 1},
		{`response.data.quota > 0 ? float(response.value) / int(2.9) : 0`, 0.5},
	} {
		t.Run(test.extract, func(t *testing.T) {
			balance, err := runBalanceExtractExpr(test.extract, []byte(`{"data":{"quota":250000},"value":"1"}`))
			require.NoError(t, err)
			assert.Equal(t, test.want, balance)
		})
	}
}

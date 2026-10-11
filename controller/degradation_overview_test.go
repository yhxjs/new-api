package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/degradation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDegradationOverviewTest(t *testing.T, targets, probes string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.DegradationCheckLog{}, &model.SystemTask{}, &model.SystemTaskLock{}))
	origDB := model.DB
	model.DB = db
	oldSetting := degradation_setting.GetDegradationSetting()
	oldJSON, err := common.Marshal(oldSetting)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	err = config.UpdateConfigFromMap(config.GlobalConfig.Get("degradation_setting"), map[string]string{
		"targets": targets, "probes": probes,
	})
	common.OptionMapRWMutex.Unlock()
	require.NoError(t, err)
	overviewCacheMutex.Lock()
	oldCache, oldAt := cachedOverview, cachedAt
	cachedOverview = nil
	overviewCacheMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		err := common.Unmarshal(oldJSON, config.GlobalConfig.Get("degradation_setting"))
		common.OptionMapRWMutex.Unlock()
		assert.NoError(t, err)
		overviewCacheMutex.Lock()
		cachedOverview, cachedAt = oldCache, oldAt
		overviewCacheMutex.Unlock()
		model.DB = origDB
		assert.NoError(t, sqlDB.Close())
	})
}

func getDegradationOverviewForTest(t *testing.T) DegradationOverviewResponse {
	t.Helper()
	RefreshDegradationOverviewCache()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/degradation/overview", nil)
	c.Set("role", common.RoleAdminUser)
	GetDegradationOverview(c)
	require.Equal(t, http.StatusOK, w.Code)
	var response struct {
		Success bool                        `json:"success"`
		Data    DegradationOverviewResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
	require.True(t, response.Success)
	return response.Data
}

func TestDegradationOverviewEmptyCollections(t *testing.T) {
	for _, tc := range []struct{ name, targets, models, probes string }{
		{"no targets", `[]`, "m1", `[]`},
		{"no probes", `[{"channel_id":98001,"models":["m1"]}]`, "m1", `[]`},
		{"no channel models", `[{"channel_id":98001}]`, "", `[]`},
		{"deleted bound probe", `[{"channel_id":98001,"models":["m1"],"bindings":[{"model_name":"m1","enabled":true,"probe_ids":["deleted"]}]}]`, "m1", `[{"id":"p1","name":"Probe"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupDegradationOverviewTest(t, tc.targets, tc.probes)
			require.NoError(t, model.DB.Create(&model.Channel{Id: 98001, Name: "test", Models: tc.models, Status: common.ChannelStatusEnabled}).Error)
			response := getDegradationOverviewForTest(t)
			require.NotNil(t, response.Channels)
			for _, channel := range response.Channels {
				require.NotNil(t, channel.Models)
				for _, item := range channel.Models {
					assert.NotNil(t, item.Probes)
					assert.Empty(t, item.Probes)
				}
			}
		})
	}
}

func TestDegradationOverviewRetainsExecutionWeights(t *testing.T) {
	setupDegradationOverviewTest(t, `[{"channel_id":98002,"models":["m1","m2"]}]`, `[{"id":"p1","name":"Probe"}]`)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 98002, Name: "test", Models: "m1,m2", Status: common.ChannelStatusEnabled}).Error)
	logs := []*model.DegradationCheckLog{
		{ChannelId: 98002, ModelName: "m1", ProbeId: "p1", Status: model.DegradationStatusPassed},
		{ChannelId: 98002, ModelName: "m1", ProbeId: "p1", Status: model.DegradationStatusPassed},
		{ChannelId: 98002, ModelName: "m1", ProbeId: "p1", Status: model.DegradationStatusPassed},
		{ChannelId: 98002, ModelName: "m2", ProbeId: "p1", Status: model.DegradationStatusMismatched},
	}
	_, err := model.RecordBatchDegradationLogs(logs)
	require.NoError(t, err)
	response := getDegradationOverviewForTest(t)
	require.Len(t, response.Channels, 1)
	require.Len(t, response.Channels[0].Models, 2)
	assert.Equal(t, int64(3), response.Channels[0].Models[0].TotalExecutions)
	assert.Equal(t, int64(1), response.Channels[0].Models[1].TotalExecutions)
	assert.Equal(t, 75.0, response.Stats.PassRate)
}

func TestDegradationOverviewAssociatesFullModelName(t *testing.T) {
	name := strings.Repeat("m", 129)
	targets, err := common.Marshal([]degradation_setting.DetectionTarget{{ChannelID: 98003, Models: []string{name}}})
	require.NoError(t, err)
	setupDegradationOverviewTest(t, string(targets), `[{"id":"p1","name":"Probe"}]`)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 98003, Name: "test", Models: name, Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, model.RecordDegradationLog(&model.DegradationCheckLog{ChannelId: 98003, ModelName: name, ProbeId: "p1", Status: model.DegradationStatusMismatched, CreatedAt: time.Now()}))
	response := getDegradationOverviewForTest(t)
	require.Len(t, response.Channels, 1)
	require.Len(t, response.Channels[0].Models, 1)
	assert.Equal(t, 0.0, response.Channels[0].Models[0].PassRate)
	assert.Equal(t, 1, response.Stats.TotalExecutions)
}

func TestDegradationOverviewUsesConfiguredChannelName(t *testing.T) {
	setupDegradationOverviewTest(t, `[{"channel_id":980031,"channel_name":"Configured label","models":["m1"]}]`, `[{"id":"p1","name":"Probe"}]`)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 980031, Name: "Database label", Models: "m1", Status: common.ChannelStatusEnabled}).Error)
	response := computeDegradationOverview()
	require.Len(t, response.Channels, 1)
	assert.Equal(t, "Configured label", response.Channels[0].ChannelName)
}

func TestDegradationOverviewRespectsChannelReadPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupDegradationOverviewTest(t,
		`[{"channel_id":98004,"models":["m1"],"bindings":[{"model_name":"m1","enabled":true,"reasoning_effort":"high"}]}]`,
		`[{"id":"p1","name":"Probe"}]`,
	)
	previousMaster, previousRedis := common.IsMasterNode, common.RedisEnabled
	common.IsMasterNode, common.RedisEnabled = true, false
	t.Cleanup(func() { common.IsMasterNode, common.RedisEnabled = previousMaster, previousRedis })
	require.NoError(t, model.DB.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(model.DB))
	require.NoError(t, model.DB.Create(&model.Channel{Id: 98004, Name: "Private Channel", Models: "m1", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, model.RecordDegradationLog(&model.DegradationCheckLog{
		ChannelId: 98004, ModelName: "m1", ProbeId: "p1", Status: model.DegradationStatusPassed,
		CreatedAt: time.Now(), ReasoningTokens: 120, Detail: "private diagnostic",
	}))
	RefreshDegradationOverviewCache()

	for _, maskTopology := range []bool{true, false} {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
			"degradation_setting.public_mask_topology": strconv.FormatBool(maskTopology),
		}))
		for _, test := range []struct {
			name        string
			role        int
			revoked     bool
			wantDetails bool
		}{
			{name: "authorized admin", role: common.RoleAdminUser, wantDetails: true},
			{name: "revoked admin", role: common.RoleAdminUser, revoked: true},
			{name: "regular user", role: common.RoleCommonUser},
			{name: "root bypass", role: common.RoleRootUser, revoked: true, wantDetails: true},
		} {
			t.Run(test.name+"/mask="+strconv.FormatBool(maskTopology), func(t *testing.T) {
				granted := !test.revoked && test.role >= common.RoleAdminUser
				require.NoError(t, authz.SetUserPermissions(101, authz.PermissionsMap{
					authz.ResourceChannel: {authz.ActionRead: granted},
				}))
				require.Equal(t, test.wantDetails, authz.Can(101, test.role, authz.ChannelRead))
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				c.Request = httptest.NewRequest(http.MethodGet, "/api/degradation/overview", nil)
				c.Set("id", 101)
				c.Set("role", test.role)
				GetDegradationOverview(c)
				var body struct {
					Success bool                        `json:"success"`
					Data    DegradationOverviewResponse `json:"data"`
				}
				require.Equal(t, http.StatusOK, response.Code)
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
				require.True(t, body.Success)
				require.Len(t, body.Data.Channels, 1)
				channel := body.Data.Channels[0]
				if maskTopology && !test.wantDetails {
					assert.Zero(t, channel.ChannelID)
					assert.NotEqual(t, "Private Channel", channel.ChannelName)
				} else {
					assert.Equal(t, 98004, channel.ChannelID)
					assert.Equal(t, "Private Channel", channel.ChannelName)
				}
				require.Len(t, channel.Models, 1)
				item := channel.Models[0]
				require.NotNil(t, item.LatestSnapshot)
				assert.Equal(t, int64(1), item.TotalExecutions)
				if test.wantDetails {
					assert.Equal(t, "high", item.ReasoningEffort)
					assert.Equal(t, "high", item.LatestSnapshot.ReasoningEffort)
					assert.Equal(t, 120, item.LatestSnapshot.ReasoningTokens)
					assert.Equal(t, "private diagnostic", item.LatestSnapshot.Detail)
				} else {
					assert.Empty(t, item.ReasoningEffort)
					assert.Empty(t, item.LatestSnapshot.ReasoningEffort)
					assert.Zero(t, item.LatestSnapshot.ReasoningTokens)
					assert.Empty(t, item.LatestSnapshot.Detail)
				}
			})
		}
	}
}

func TestDegradationOverviewExcludesInactiveChannelsAndRetainsHistory(t *testing.T) {
	for _, tc := range []struct {
		name          string
		channelStatus int
		modelEnabled  bool
		wantActive    int
	}{
		{"enabled", common.ChannelStatusEnabled, true, 1},
		{"manually disabled", common.ChannelStatusManuallyDisabled, true, 0},
		{"automatically disabled", common.ChannelStatusAutoDisabled, true, 0},
		{"paused model", common.ChannelStatusEnabled, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupDegradationOverviewTest(t,
				`[{"channel_id":98005,"models":["m1"],"bindings":[{"model_name":"m1","enabled":`+strconv.FormatBool(tc.modelEnabled)+`}]}]`,
				`[{"id":"p1","name":"Probe"}]`,
			)
			require.NoError(t, model.DB.Create(&model.Channel{Id: 98005, Name: "test", Models: "m1", Status: tc.channelStatus}).Error)
			require.NoError(t, model.RecordDegradationLog(&model.DegradationCheckLog{
				ChannelId: 98005, ModelName: "m1", ProbeId: "p1", Status: model.DegradationStatusMismatched,
			}))

			response := getDegradationOverviewForTest(t)
			assert.Equal(t, tc.wantActive, response.Stats.ActiveTargets)
			assert.Equal(t, tc.wantActive, response.Stats.TotalExecutions)
			assert.Equal(t, tc.wantActive, response.Stats.MismatchCount)
			require.Len(t, response.Channels, 1)
			require.Len(t, response.Channels[0].Models, 1)
			item := response.Channels[0].Models[0]
			assert.Equal(t, tc.wantActive > 0, item.Enabled)
			assert.Equal(t, int64(1), item.TotalExecutions)
			require.NotNil(t, item.LatestSnapshot)
			assert.Equal(t, model.DegradationStatusMismatched, item.LatestSnapshot.Status)

			if tc.channelStatus != common.ChannelStatusEnabled {
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 98005).Update("status", common.ChannelStatusEnabled).Error)
				response = getDegradationOverviewForTest(t)
				assert.Equal(t, 1, response.Stats.ActiveTargets)
				assert.Equal(t, 1, response.Stats.TotalExecutions)
				assert.True(t, response.Channels[0].Models[0].Enabled)
			}
		})
	}
}

package router

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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

func setupDegradationRouterTest(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Channel{}, &model.CasbinRule{}, &model.AuthzRole{},
		&model.SystemTask{}, &model.SystemTaskLock{}, &model.DegradationCheckLog{},
	))
	previousDB := model.DB
	previousType := common.MainDatabaseType()
	previousMaster, previousRedis, previousRateLimit := common.IsMasterNode, common.RedisEnabled, common.GlobalApiRateLimitEnable
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.IsMasterNode, common.RedisEnabled, common.GlobalApiRateLimitEnable = true, false, false
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
		common.IsMasterNode, common.RedisEnabled, common.GlobalApiRateLimitEnable = previousMaster, previousRedis, previousRateLimit
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, authz.Init(db))

	previousSetting := degradation_setting.GetDegradationSetting()
	previousProbes, err := common.Marshal(previousSetting.Probes)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
			"degradation_setting.enabled": strconv.FormatBool(previousSetting.Enabled),
			"degradation_setting.probes":  string(previousProbes),
		}))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"degradation_setting.enabled": "true",
		"degradation_setting.probes":  `[{"id":"probe","name":"Probe","prompt":"Answer OK","keywords":["OK"],"match_rule":"any","max_tokens":10}]`,
	}))
	require.NoError(t, db.Create(&model.Channel{Id: 1, Name: "Test Channel", Models: "test-model", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, model.RecordDegradationLog(&model.DegradationCheckLog{
		CreatedAt: time.Now(), ChannelId: 1, ChannelName: "Test Channel",
		ModelName: "test-model", ProbeId: "probe", ProbeName: "Probe",
		Status: model.DegradationStatusPassed, Detail: "private probe detail",
	}))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// Authorization tests do not exercise asynchronous audit persistence.
	engine.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyAuditLogged, true)
		c.Next()
	})
	SetApiRouter(engine)
	return engine, db
}

func TestDegradationRoutesEnforceChannelPermissions(t *testing.T) {
	for _, route := range []struct {
		name       string
		method     string
		path       string
		body       string
		permission authz.Permission
	}{
		{name: "logs", method: http.MethodGet, path: "/api/degradation/logs", permission: authz.ChannelRead},
		{name: "full run", method: http.MethodPost, path: "/api/degradation/run", permission: authz.ChannelOperate},
		{name: "target run", method: http.MethodPost, path: "/api/degradation/run-target", body: `{"channel_id":1,"model_name":"test-model"}`, permission: authz.ChannelOperate},
	} {
		for _, test := range []struct {
			name       string
			role       int
			revoked    bool
			wantStatus int
		}{
			{name: "authorized admin", role: common.RoleAdminUser, wantStatus: http.StatusOK},
			{name: "revoked admin", role: common.RoleAdminUser, revoked: true, wantStatus: http.StatusForbidden},
			{name: "regular user", role: common.RoleCommonUser, wantStatus: http.StatusForbidden},
			{name: "root bypass", role: common.RoleRootUser, revoked: true, wantStatus: http.StatusOK},
		} {
			t.Run(route.name+"/"+test.name, func(t *testing.T) {
				engine, db := setupDegradationRouterTest(t)
				token := "degradation-admin-pat"
				user := &model.User{
					Username: "degradation-user", Password: "unused-password-hash", Role: test.role,
					Status: common.UserStatusEnabled, Group: "default", AccessToken: &token, AuthVersion: 1,
				}
				require.NoError(t, db.Create(user).Error)
				if test.revoked {
					require.NoError(t, authz.SetUserPermissions(user.Id, authz.PermissionsMap{
						route.permission.Resource: {route.permission.Action: false},
					}))
				}
				require.Equal(t, test.wantStatus == http.StatusOK, authz.Can(user.Id, test.role, route.permission))

				request := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
				request.Header.Set("Authorization", "Bearer "+token)
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				engine.ServeHTTP(response, request)

				assert.Equal(t, test.wantStatus, response.Code)
				var body struct {
					Success bool `json:"success"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
				assert.Equal(t, test.wantStatus == http.StatusOK, body.Success)
				var taskCount int64
				require.NoError(t, db.Model(&model.SystemTask{}).Count(&taskCount).Error)
				if route.method == http.MethodPost && test.wantStatus == http.StatusOK {
					assert.EqualValues(t, 1, taskCount)
				} else {
					assert.Zero(t, taskCount)
				}
				if route.method == http.MethodGet && test.wantStatus == http.StatusOK {
					assert.Contains(t, response.Body.String(), "private probe detail")
				} else {
					assert.NotContains(t, response.Body.String(), "private probe detail")
				}
			})
		}
	}
}

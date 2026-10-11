package controller

import (
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDegradationOverviewUsesPersistedSchedule(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   model.SystemTaskStatus
		age      time.Duration
		manual   bool
		enabled  bool
		wantSecs int
	}{
		{name: "no previous round is due immediately", enabled: true},
		{name: "completed round survives restart", status: model.SystemTaskStatusSucceeded, age: 5 * time.Minute, enabled: true, wantSecs: 300},
		{name: "failed round follows scheduler cadence", status: model.SystemTaskStatusFailed, age: 5 * time.Minute, enabled: true, wantSecs: 300},
		{name: "overdue round", status: model.SystemTaskStatusSucceeded, age: 15 * time.Minute, enabled: true},
		{name: "pending round", status: model.SystemTaskStatusPending, enabled: true},
		{name: "running round", status: model.SystemTaskStatusRunning, enabled: true},
		{name: "manual target does not postpone full round", status: model.SystemTaskStatusSucceeded, age: 5 * time.Minute, manual: true, enabled: true, wantSecs: 300},
		{name: "disabled schedule", status: model.SystemTaskStatusSucceeded, age: 5 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupDegradationOverviewTest(t, `[]`, `[]`)
			if dialect := os.Getenv("DEGRADATION_DIALECT_DB"); dialect == "mysql" || dialect == "postgres" {
				var dialector gorm.Dialector
				if dialect == "mysql" {
					dialector = mysql.Open(os.Getenv("DEGRADATION_DIALECT_DSN"))
				} else {
					dialector = postgres.Open(os.Getenv("DEGRADATION_DIALECT_DSN"))
				}
				db, err := gorm.Open(dialector, &gorm.Config{})
				require.NoError(t, err)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				previousDB := model.DB
				model.DB = db
				t.Cleanup(func() {
					model.DB = previousDB
					assert.NoError(t, sqlDB.Close())
				})
			}
			require.NoError(t, model.DB.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"degradation_setting.enabled":          "false",
				"degradation_setting.interval_minutes": "10",
			}))
			if tc.enabled {
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"degradation_setting.enabled": "true"}))
			}
			if tc.status != "" {
				task, err := model.CreateSystemTask(model.SystemTaskTypeDegradationCheck, nil, nil)
				require.NoError(t, err)
				t.Cleanup(func() {
					assert.NoError(t, model.DB.Where("task_id = ?", task.TaskID).Delete(&model.SystemTask{}).Error)
				})
				require.NoError(t, model.DB.Model(task).Updates(map[string]any{
					"status": tc.status, "updated_at": time.Now().Add(-tc.age).Unix(), "active_key": nil,
				}).Error)
				if tc.manual {
					manual, err := model.CreateSystemTask(model.SystemTaskTypeDegradationCheck, map[string]any{"channel_id": 7, "model_name": "gpt-5"}, nil)
					require.NoError(t, err)
					t.Cleanup(func() {
						assert.NoError(t, model.DB.Where("task_id = ?", manual.TaskID).Delete(&model.SystemTask{}).Error)
					})
					require.NoError(t, model.DB.Model(manual).Updates(map[string]any{
						"status": model.SystemTaskStatusSucceeded, "updated_at": time.Now().Unix(), "active_key": nil,
					}).Error)
				}
			}

			overview := computeDegradationOverview()

			assert.InDelta(t, tc.wantSecs, overview.Stats.NextRunSeconds, 1)
			assert.False(t, overview.Stats.AggregationError)
		})
	}
}

func TestDegradationOverviewScheduleQueryFailure(t *testing.T) {
	setupDegradationOverviewTest(t, `[]`, `[]`)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"degradation_setting.enabled": "true"}))
	require.NoError(t, model.DB.Migrator().DropTable(&model.SystemTask{}))

	overview := computeDegradationOverview()

	assert.True(t, overview.Stats.AggregationError)
	assert.Zero(t, overview.Stats.NextRunSeconds)
}

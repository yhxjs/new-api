package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/degradation_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

func setupDegradationTaskTest(t *testing.T) *model.Channel {
	t.Helper()
	setupDegradationOverviewTest(t,
		`[{"channel_id":98011,"models":["gpt-4o-mini"]}]`,
		`[{"id":"p1","name":"Probe","prompt":"Reply with ok","keywords":["ok"],"max_tokens":16}]`)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.SystemTask{}, &model.SystemTaskLock{}))
	oldLogDB := model.LOG_DB
	oldLogType := common.LogDatabaseType()
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	oldRedis, oldLogEnabled := common.RedisEnabled, common.LogConsumeEnabled
	common.RedisEnabled, common.LogConsumeEnabled = false, false
	t.Cleanup(func() {
		model.LOG_DB = oldLogDB
		common.SetLogDatabaseType(oldLogType)
		common.RedisEnabled, common.LogConsumeEnabled = oldRedis, oldLogEnabled
	})
	common.OptionMapRWMutex.Lock()
	err := config.UpdateConfigFromMap(config.GlobalConfig.Get("degradation_setting"), map[string]string{"enabled": "true"})
	common.OptionMapRWMutex.Unlock()
	require.NoError(t, err)
	userSetting, err := common.Marshal(dto.UserSetting{AcceptUnsetRatioModel: true})
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.User{
		Username: "probe-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled,
		Group: "default", Setting: string(userSetting),
	}).Error)
	channel := &model.Channel{
		Id: 98011, Name: "Probe channel", Type: constant.ChannelTypeOpenAI,
		Status: common.ChannelStatusEnabled, Models: "gpt-4o-mini", Key: "test-key",
	}
	return channel
}

func TestDegradationTaskChannelLookupFailure(t *testing.T) {
	for _, mode := range []string{"full round", "target run"} {
		t.Run(mode, func(t *testing.T) {
			channel := setupDegradationTaskTest(t)
			require.NoError(t, model.DB.Create(channel).Error)
			var payload any
			if mode == "target run" {
				payload = degradationTargetFilter{ChannelID: channel.Id, ModelName: "gpt-4o-mini"}
			}
			task, err := model.CreateSystemTask(model.SystemTaskTypeDegradationCheck, payload, nil)
			require.NoError(t, err)
			task, claimed, err := model.ClaimSystemTask(task.ID, task.Type, "probe-runner", common.GetTimestamp()+60)
			require.NoError(t, err)
			require.True(t, claimed)
			require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("test:fail_degradation_channel_lookup", func(tx *gorm.DB) {
				if tx.Statement.Table == "channels" {
					tx.AddError(errors.New("channel query temporarily unavailable"))
				}
			}))

			(degradationCheckHandler{}).Run(context.Background(), task, "probe-runner")

			stored, err := model.GetSystemTaskByTaskID(task.TaskID)
			require.NoError(t, err)
			assert.Equal(t, model.SystemTaskStatusFailed, stored.Status)
			assert.Contains(t, stored.Error, "channel query temporarily unavailable")
			assert.Contains(t, stored.Error, strconv.Itoa(channel.Id))
			var logs int64
			require.NoError(t, model.DB.Model(&model.DegradationCheckLog{}).Count(&logs).Error)
			assert.Zero(t, logs)
		})
	}
}

func TestDegradationTaskSkipsDeletedChannel(t *testing.T) {
	for _, mode := range []string{"full round", "target run"} {
		t.Run(mode, func(t *testing.T) {
			channel := setupDegradationTaskTest(t)
			require.NoError(t, model.DB.Create(channel).Error)
			require.NoError(t, model.DB.Delete(channel).Error)
			var filter degradationTargetFilter
			wantReason := "no_active_targets"
			if mode == "target run" {
				filter = degradationTargetFilter{ChannelID: channel.Id, ModelName: "gpt-4o-mini"}
				wantReason = "target_channel_not_found"
			}

			result, err := runDegradationCheckTask(context.Background(), func(int, int) {}, filter)

			require.NoError(t, err)
			assert.Equal(t, map[string]any{"skipped": true, "reason": wantReason}, result)
		})
	}
}

func TestDegradationTaskPersistenceControlsSuccess(t *testing.T) {
	for _, failure := range []string{"persistence", "cleanup", "none"} {
		t.Run(failure, func(t *testing.T) {
			channel := setupDegradationTaskTest(t)
			var requests atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`))
			}))
			t.Cleanup(upstream.Close)
			channel.BaseURL = lo.ToPtr(upstream.URL)
			require.NoError(t, model.DB.Create(channel).Error)
			service.InitHttpClient()
			if failure == "persistence" {
				require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("test:fail_degradation_logs", func(tx *gorm.DB) {
					if tx.Statement.Table == "degradation_check_logs" {
						tx.AddError(errors.New("forced degradation persistence failure"))
					}
				}))
			}
			if failure == "cleanup" {
				require.NoError(t, model.RecordDegradationLog(&model.DegradationCheckLog{
					ChannelId: channel.Id, ModelName: "old-model", ProbeId: "p1",
					CreatedAt: time.Now().AddDate(0, 0, -degradation_setting.MaxRetentionDays-1),
				}))
				require.NoError(t, model.DB.Callback().Delete().Before("gorm:delete").Register("test:fail_degradation_cleanup", func(tx *gorm.DB) {
					if tx.Statement.Table == "degradation_check_logs" {
						tx.AddError(errors.New("forced degradation cleanup failure"))
					}
				}))
			}
			sentinel := &DegradationOverviewResponse{}
			sentinelAt := time.Unix(10, 0)
			overviewCacheMutex.Lock()
			cachedOverview, cachedAt = sentinel, sentinelAt
			overviewCacheMutex.Unlock()
			task, err := model.CreateSystemTask(model.SystemTaskTypeDegradationCheck, nil, nil)
			require.NoError(t, err)
			task, claimed, err := model.ClaimSystemTask(task.ID, task.Type, "probe-runner", common.GetTimestamp()+60)
			require.NoError(t, err)
			require.True(t, claimed)

			(degradationCheckHandler{}).Run(context.Background(), task, "probe-runner")

			stored, err := model.GetSystemTaskByTaskID(task.TaskID)
			require.NoError(t, err)
			assert.Equal(t, int64(1), requests.Load(), "the probe must actually execute before persistence")
			if failure != "none" {
				assert.Equal(t, model.SystemTaskStatusFailed, stored.Status)
				assert.Contains(t, stored.Error, "forced degradation "+failure+" failure")
				overviewCacheMutex.RLock()
				assert.Same(t, sentinel, cachedOverview)
				assert.Equal(t, sentinelAt, cachedAt)
				overviewCacheMutex.RUnlock()
				return
			}
			assert.Equal(t, model.SystemTaskStatusSucceeded, stored.Status)
			overviewCacheMutex.RLock()
			assert.InDelta(t, (degradationCheckHandler{}).Interval().Seconds(), cachedOverview.Stats.NextRunSeconds, 1)
			overviewCacheMutex.RUnlock()
			var log model.DegradationCheckLog
			require.NoError(t, model.DB.First(&log).Error)
			assert.Equal(t, model.DegradationStatusPassed, log.Status)
			assert.Equal(t, 1, log.TokenCount)
		})
	}
}

func TestDegradationTaskUsesConfiguredChannelName(t *testing.T) {
	channel := setupDegradationTaskTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`))
	}))
	t.Cleanup(upstream.Close)
	channel.BaseURL = lo.ToPtr(upstream.URL)
	require.NoError(t, model.DB.Create(channel).Error)
	service.InitHttpClient()

	setting := degradation_setting.GetDegradationSetting()
	setting.Targets[0].ChannelName = "Configured probe label"
	common.OptionMapRWMutex.Lock()
	*config.GlobalConfig.Get("degradation_setting").(*degradation_setting.DegradationSetting) = setting
	common.OptionMapRWMutex.Unlock()

	_, err := runDegradationCheckTask(context.Background(), func(_, _ int) {}, degradationTargetFilter{})
	require.NoError(t, err)

	var log model.DegradationCheckLog
	require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).First(&log).Error)
	assert.Equal(t, "Configured probe label", log.ChannelName)
}

func TestDegradationTaskCapsExpandedAllModelWorkBeforeExecution(t *testing.T) {
	channel := setupDegradationTaskTest(t)
	models := make([]string, 200)
	for i := range models {
		models[i] = fmt.Sprintf("model-%d", i)
	}
	channel.Models = strings.Join(models, ",")
	require.NoError(t, model.DB.Create(channel).Error)
	setting := degradation_setting.GetDegradationSetting()
	setting.Probes = make([]degradation_setting.ProbeConfig, 51)
	for i := range setting.Probes {
		setting.Probes[i] = degradation_setting.ProbeConfig{ID: fmt.Sprintf("probe-%d", i), Name: "Probe", Prompt: "Reply ok", Keywords: []string{"ok"}, MaxTokens: 16}
	}
	setting.Targets = []degradation_setting.DetectionTarget{{ChannelID: channel.Id}}
	common.OptionMapRWMutex.Lock()
	*config.GlobalConfig.Get("degradation_setting").(*degradation_setting.DegradationSetting) = setting
	common.OptionMapRWMutex.Unlock()
	require.Empty(t, degradation_setting.GetDegradationSetting().Targets[0].Models)
	// Cancellation keeps a regression from issuing thousands of upstream requests.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := runDegradationCheckTask(ctx, func(_, _ int) {
		t.Error("oversized rounds must not start execution")
	}, degradationTargetFilter{})

	require.ErrorContains(t, err, "task count exceeds")
	assert.Nil(t, result)
}

func TestDegradationTaskAlreadyCanceled(t *testing.T) {
	channel := setupDegradationTaskTest(t)
	require.NoError(t, model.DB.Create(channel).Error)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runDegradationCheckTask(ctx, func(int, int) { t.Error("canceled probes must not execute") }, degradationTargetFilter{})

	require.ErrorIs(t, err, context.Canceled)
}

func TestDegradationTaskCancellationPreservesCompletedLogs(t *testing.T) {
	channel := setupDegradationTaskTest(t)
	var requests atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`))
	}))
	t.Cleanup(upstream.Close)
	channel.BaseURL = lo.ToPtr(upstream.URL)
	require.NoError(t, model.DB.Create(channel).Error)
	service.InitHttpClient()
	setting := degradation_setting.GetDegradationSetting()
	second := setting.Probes[0]
	second.ID = "p2"
	setting.Probes = append(setting.Probes, second)
	common.OptionMapRWMutex.Lock()
	*config.GlobalConfig.Get("degradation_setting").(*degradation_setting.DegradationSetting) = setting
	common.OptionMapRWMutex.Unlock()
	oldLog := &model.DegradationCheckLog{
		ChannelId: channel.Id, ModelName: "old-model", ProbeId: "p1",
		CreatedAt: time.Now().AddDate(0, 0, -degradation_setting.MaxRetentionDays-1),
	}
	require.NoError(t, model.RecordDegradationLog(oldLog))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	task, err := model.CreateSystemTask(model.SystemTaskTypeDegradationCheck, nil, nil)
	require.NoError(t, err)
	task, claimed, err := model.ClaimSystemTask(task.ID, task.Type, "probe-runner", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	// Cancel after the first probe's progress is saved, before the inter-probe delay.
	require.NoError(t, model.DB.Callback().Update().After("gorm:update").Register("test:cancel_degradation_round", func(tx *gorm.DB) {
		if tx.Statement.Table != "system_tasks" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		state, _ := updates["state"].(string)
		if gjson.Get(state, "processed").Int() == 1 {
			cancel()
		}
	}))

	(degradationCheckHandler{}).Run(ctx, task, "probe-runner")

	require.ErrorIs(t, ctx.Err(), context.Canceled, "the fixture must cancel after one completed probe")
	stored, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, model.SystemTaskStatusFailed, stored.Status)
	assert.Contains(t, stored.Error, context.Canceled.Error())
	assert.Equal(t, int64(1), requests.Load(), "the second probe must not execute")
	var logs []model.DegradationCheckLog
	require.NoError(t, model.DB.Order("id").Find(&logs).Error)
	require.Len(t, logs, 2, "cancellation must preserve old and completed logs without cleanup")
	assert.Equal(t, "old-model", logs[0].ModelName)
	assert.Equal(t, "p1", logs[1].ProbeId)
	assert.Equal(t, model.DegradationStatusPassed, logs[1].Status)
}

func TestRunTargetDegradationCheckRejectsTargetAddedDuringFullRound(t *testing.T) {
	channel := setupDegradationTaskTest(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`))
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	channel.BaseURL = lo.ToPtr(upstream.URL)
	require.NoError(t, model.DB.Create(channel).Error)
	newChannel := *channel
	newChannel.Id++
	newChannel.Name = "Added target"
	require.NoError(t, model.DB.Create(&newChannel).Error)
	service.InitHttpClient()
	task, err := model.CreateSystemTask(model.SystemTaskTypeDegradationCheck, nil, nil)
	require.NoError(t, err)
	task, claimed, err := model.ClaimSystemTask(task.ID, task.Type, "probe-runner", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		(degradationCheckHandler{}).Run(ctx, task, "probe-runner")
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("the original target must start before changing the configuration")
	}
	setting := degradation_setting.GetDegradationSetting()
	setting.Targets = append(setting.Targets, degradation_setting.DetectionTarget{
		ChannelID: newChannel.Id, Models: []string{"gpt-4o-mini"},
	})
	common.OptionMapRWMutex.Lock()
	*config.GlobalConfig.Get("degradation_setting").(*degradation_setting.DegradationSetting) = setting
	common.OptionMapRWMutex.Unlock()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/degradation/run-target", strings.NewReader(fmt.Sprintf(
		`{"channel_id":%d,"model_name":"gpt-4o-mini"}`, newChannel.Id,
	)))
	c.Request.Header.Set("Content-Type", "application/json")
	RunTargetDegradationCheck(c)
	close(release)
	<-done
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
	assert.False(t, response.Success, "a running round cannot claim a newly configured target")
	assert.Contains(t, response.Message, "retry after it finishes")
	var logs int64
	require.NoError(t, model.DB.Model(&model.DegradationCheckLog{}).Where("channel_id = ?", newChannel.Id).Count(&logs).Error)
	assert.Zero(t, logs, "the old round only executes its original target snapshot")
}

func TestRunTargetDegradationCheckRejectsPendingFullRound(t *testing.T) {
	channel := setupDegradationTaskTest(t)
	require.NoError(t, model.DB.Create(channel).Error)
	_, err := model.CreateSystemTask(model.SystemTaskTypeDegradationCheck, nil, nil)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/degradation/run-target", strings.NewReader(fmt.Sprintf(
		`{"channel_id":%d,"model_name":"gpt-4o-mini"}`, channel.Id,
	)))
	c.Request.Header.Set("Content-Type", "application/json")
	RunTargetDegradationCheck(c)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
	assert.False(t, response.Success, "a pending round has not captured its execution targets")
	assert.Contains(t, response.Message, "retry after it finishes")
}

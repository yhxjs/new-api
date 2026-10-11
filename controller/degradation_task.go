package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/degradation_setting"
	"gorm.io/gorm"
)

type degradationChannelTaskGroup struct {
	channelID   int
	channel     *model.Channel
	channelName string
	tasks       []degradationTaskItem
}

type degradationTaskItem struct {
	modelName string
	probe     degradation_setting.ProbeConfig
	binding   *degradation_setting.ModelProbeBinding
}

// applicableProbesForBinding resolves the probes that apply to one model: a
// binding with explicit probe_ids wins (unknown ids are dropped), otherwise
// every configured probe applies. Shared by the executor and the overview so
// their binding semantics cannot drift.
func applicableProbesForBinding(
	setting degradation_setting.DegradationSetting,
	probesMap map[string]degradation_setting.ProbeConfig,
	binding *degradation_setting.ModelProbeBinding,
) []degradation_setting.ProbeConfig {
	if binding != nil && len(binding.ProbeIDs) > 0 {
		var probes []degradation_setting.ProbeConfig
		for _, pid := range binding.ProbeIDs {
			if p, ok := probesMap[pid]; ok {
				probes = append(probes, p)
			}
		}
		return probes
	}
	return setting.Probes
}

// degradationTargetFilter limits a manual run to a single channel/model. A
// zero ChannelID means a full scheduled round over all configured targets.
type degradationTargetFilter struct {
	ChannelID int    `json:"channel_id"`
	ModelName string `json:"model_name"`
}

var degradationRunMutex sync.Mutex

func runDegradationCheckTask(ctx context.Context, reporter func(processed, total int), filter degradationTargetFilter) (map[string]any, error) {
	if !degradationRunMutex.TryLock() {
		common.SysLog("degradation check task is already running, skipping this round")
		return map[string]any{"skipped": true, "reason": "already_running"}, nil
	}
	defer degradationRunMutex.Unlock()

	setting := degradation_setting.GetDegradationSetting()
	if !setting.Enabled {
		return map[string]any{"skipped": true, "reason": "disabled"}, nil
	}

	if len(setting.Probes) == 0 {
		return map[string]any{"skipped": true, "reason": "no_probes"}, nil
	}

	probesMap := make(map[string]degradation_setting.ProbeConfig)
	for _, p := range setting.Probes {
		probesMap[p.ID] = p
	}

	var groups []degradationChannelTaskGroup
	totalTasksCount := 0

	if filter.ChannelID > 0 {
		// 手动单测：忽略调度目标与渠道启停状态，按绑定配置（若有）探测指定模型
		channel, err := model.GetChannelById(filter.ChannelID, true)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("load degradation channel %d: %w", filter.ChannelID, err)
		}
		if channel == nil {
			return map[string]any{"skipped": true, "reason": "target_channel_not_found"}, nil
		}

		bindingsMap := make(map[string]degradation_setting.ModelProbeBinding)
		for _, target := range setting.Targets {
			if target.ChannelID == filter.ChannelID {
				for _, b := range target.Bindings {
					bindingsMap[b.ModelName] = b
				}
				break
			}
		}

		var bindingPtr *degradation_setting.ModelProbeBinding
		channelName := channel.Name
		if binding, ok := bindingsMap[filter.ModelName]; ok {
			bCopy := binding
			bindingPtr = &bCopy
		}
		for _, target := range setting.Targets {
			if target.ChannelID == filter.ChannelID {
				channelName = configuredDegradationChannelName(target, channel)
				break
			}
		}

		applicableProbes := applicableProbesForBinding(setting, probesMap, bindingPtr)

		var taskItems []degradationTaskItem
		for _, p := range applicableProbes {
			if totalTasksCount+len(taskItems) >= degradation_setting.MaxTasksPerRound {
				return nil, fmt.Errorf("degradation check task count exceeds %d", degradation_setting.MaxTasksPerRound)
			}
			taskItems = append(taskItems, degradationTaskItem{
				modelName: filter.ModelName,
				probe:     p,
				binding:   bindingPtr,
			})
		}

		if len(taskItems) > 0 {
			groups = append(groups, degradationChannelTaskGroup{
				channelID:   filter.ChannelID,
				channel:     channel,
				channelName: channelName,
				tasks:       taskItems,
			})
			totalTasksCount += len(taskItems)
		}
	} else {
		for _, target := range setting.Targets {
			channel, err := model.GetChannelById(target.ChannelID, true)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("load degradation channel %d: %w", target.ChannelID, err)
			}
			if channel == nil || channel.Status != common.ChannelStatusEnabled {
				continue
			}

			models := target.Models
			if len(models) == 0 {
				models = channel.GetModels()
			}

			bindingsMap := make(map[string]degradation_setting.ModelProbeBinding)
			for _, b := range target.Bindings {
				bindingsMap[b.ModelName] = b
			}

			var taskItems []degradationTaskItem
			for _, m := range models {
				binding, hasBinding := bindingsMap[m]
				if hasBinding && !binding.Enabled {
					continue // 模型被明确关闭/暂停巡检
				}

				var bindingPtr *degradation_setting.ModelProbeBinding
				if hasBinding {
					bCopy := binding
					bindingPtr = &bCopy
				}
				applicableProbes := applicableProbesForBinding(setting, probesMap, bindingPtr)

				for _, p := range applicableProbes {
					if totalTasksCount+len(taskItems) >= degradation_setting.MaxTasksPerRound {
						return nil, fmt.Errorf("degradation check task count exceeds %d", degradation_setting.MaxTasksPerRound)
					}
					taskItems = append(taskItems, degradationTaskItem{
						modelName: m,
						probe:     p,
						binding:   bindingPtr,
					})
				}
			}

			if len(taskItems) > 0 {
				groups = append(groups, degradationChannelTaskGroup{
					channelID:   target.ChannelID,
					channel:     channel,
					channelName: configuredDegradationChannelName(target, channel),
					tasks:       taskItems,
				})
				totalTasksCount += len(taskItems)
			}
		}
	}

	if len(groups) == 0 {
		return map[string]any{"skipped": true, "reason": "no_active_targets"}, nil
	}

	probeUserID, err := resolveChannelTestUserID(nil)
	if err != nil {
		return nil, err
	}

	concurrency := setting.Concurrency
	if concurrency < 1 {
		concurrency = degradation_setting.DefaultConcurrency
	}
	jitterMs := time.Duration(setting.ChannelJitterMs) * time.Millisecond

	timeout := time.Duration(setting.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(degradation_setting.DefaultTimeoutSeconds) * time.Second
	}

	groupChan := make(chan degradationChannelTaskGroup, len(groups))
	for _, g := range groups {
		groupChan <- g
	}
	close(groupChan)

	var wg sync.WaitGroup
	var logsMutex sync.Mutex
	var collectedLogs []*model.DegradationCheckLog

	// NewSystemTaskProgressReporter 内部持有节流状态，非并发安全，需串行调用
	var reporterMutex sync.Mutex
	var processedCount atomic.Int64
	reportProgress := func() {
		reporterMutex.Lock()
		defer reporterMutex.Unlock()
		reporter(int(processedCount.Load()), totalTasksCount)
	}

	workerCount := concurrency
	if workerCount > len(groups) {
		workerCount = len(groups)
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for g := range groupChan {
				for idx, item := range g.tasks {
					select {
					case <-ctx.Done():
						return
					default:
					}

					reqCtx, cancel := context.WithTimeout(ctx, timeout)
					res := ExecuteDegradationProbe(reqCtx, probeUserID, g.channel, item.modelName, item.probe, item.binding)
					cancel()

					logRow := &model.DegradationCheckLog{
						CreatedAt:       time.Now(),
						ChannelId:       g.channel.Id,
						ChannelName:     g.channelName,
						ModelName:       item.modelName,
						ProbeId:         item.probe.ID,
						ProbeName:       item.probe.Name,
						Status:          res.Status,
						LatencyMs:       res.LatencyMs,
						TokenCount:      res.TokenCount,
						ReasoningTokens: res.ReasoningTokens,
						Detail:          res.Detail,
					}

					logsMutex.Lock()
					collectedLogs = append(collectedLogs, logRow)
					logsMutex.Unlock()

					processedCount.Add(1)
					reportProgress()

					if idx < len(g.tasks)-1 && jitterMs > 0 {
						timer := time.NewTimer(jitterMs)
						select {
						case <-ctx.Done():
							timer.Stop()
							return
						case <-timer.C:
						}
					}
				}
			}
		}()
	}

	wg.Wait()

	persistedLogs := int64(0)
	if len(collectedLogs) > 0 {
		persisted, recordErr := model.RecordBatchDegradationLogs(collectedLogs)
		persistedLogs = persisted
		if recordErr != nil {
			common.SysError(fmt.Sprintf("failed to record degradation logs batch: %v", recordErr))
			return nil, fmt.Errorf("record degradation logs: %w", recordErr)
		}
	}
	// Keep completed probe logs even when the lease or caller canceled the round.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 触发过期清理
	retentionDays := setting.RetentionDays
	if retentionDays <= 0 {
		retentionDays = degradation_setting.DefaultRetentionDays
	}
	expireTime := time.Now().AddDate(0, 0, -retentionDays)
	deletedCount, cleanErr := model.DeleteExpiredDegradationLogs(expireTime)
	if cleanErr != nil {
		common.SysError(fmt.Sprintf("failed to clean expired degradation logs: %v", cleanErr))
		return nil, fmt.Errorf("clean expired degradation logs: %w", cleanErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return map[string]any{
		"total_targets":  len(groups),
		"total_probes":   totalTasksCount,
		"executed_logs":  len(collectedLogs),
		"persisted_logs": persistedLogs,
		"cleaned_logs":   deletedCount,
	}, nil
}

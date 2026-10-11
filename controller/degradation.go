package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/degradation_setting"
	"github.com/gin-gonic/gin"
)

type DegradationOverviewStats struct {
	PassRate        float64 `json:"pass_rate"`
	TotalExecutions int     `json:"total_executions"`
	MismatchCount   int     `json:"mismatch_count"`
	ErrorCount      int     `json:"error_count"`
	ActiveTargets   int     `json:"active_targets"`
	NextRunSeconds  int     `json:"next_run_seconds"`
	Enabled         bool    `json:"enabled"`
	// AggregationError marks a partial overview: the underlying log queries
	// failed, so the zeroed/empty figures must not be read as "all healthy".
	AggregationError bool `json:"aggregation_error"`
}

type DegradationProbeTimeline struct {
	ProbeID   string  `json:"probe_id"`
	ProbeName string  `json:"probe_name"`
	PassRate  float64 `json:"pass_rate"`
	Slots     []int   `json:"slots"` // 24 个状态格: 1=通过, 2=答案不符, 3=错误, 4=超时, 0=无数据
}

type DegradationSnapshotItem struct {
	Time            string `json:"time"`
	Status          int    `json:"status"`
	ProbeName       string `json:"probe_name"`
	LatencyMs       int    `json:"latency_ms"`
	TokenCount      int    `json:"token_count"`
	ReasoningTokens int    `json:"reasoning_tokens,omitempty"` // 仅管理员可见
	ReasoningEffort string `json:"reasoning_effort,omitempty"` // 仅管理员可见
	Detail          string `json:"detail,omitempty"`
}

type DegradationModelItem struct {
	ModelName       string                     `json:"model_name"`
	Enabled         bool                       `json:"enabled"`
	ReasoningEffort string                     `json:"reasoning_effort,omitempty"` // 仅管理员可见
	PassRate        float64                    `json:"pass_rate"`
	TotalExecutions int64                      `json:"total_executions"`
	AvgLatencyMs    int                        `json:"avg_latency_ms"`
	Probes          []DegradationProbeTimeline `json:"probes"`
	LatestSnapshot  *DegradationSnapshotItem   `json:"latest_snapshot,omitempty"`
}

type DegradationChannelGroup struct {
	ChannelID   int                    `json:"channel_id,omitempty"` // 普通用户脱敏时为 0
	ChannelName string                 `json:"channel_name"`
	Models      []DegradationModelItem `json:"models"`
}

type DegradationOverviewResponse struct {
	Stats    DegradationOverviewStats  `json:"stats"`
	Channels []DegradationChannelGroup `json:"channels"`
}

func configuredDegradationChannelName(target degradation_setting.DetectionTarget, channel *model.Channel) string {
	if name := strings.TrimSpace(target.ChannelName); name != "" {
		return name
	}
	if channel != nil {
		return channel.Name
	}
	return ""
}

var (
	overviewCacheMutex   sync.RWMutex
	overviewComputeMutex sync.Mutex
	overviewRefreshing   atomic.Bool
	cachedOverview       *DegradationOverviewResponse
	cachedAt             time.Time
)

const overviewCacheTTL = 30 * time.Second

// RefreshDegradationOverviewCache recomputes the shared overview snapshot after
// a probe round finishes. The DB work runs under the compute mutex so cache
// readers are never blocked by queries.
func RefreshDegradationOverviewCache() {
	overviewComputeMutex.Lock()
	defer overviewComputeMutex.Unlock()
	res := computeDegradationOverview()
	overviewCacheMutex.Lock()
	cachedOverview = res
	cachedAt = time.Now()
	overviewCacheMutex.Unlock()
}

// getOrComputeDegradationOverview serves the cached snapshot and, once it goes
// stale, kicks off a single-flight background recompute instead of blocking the
// request on the (potentially heavy) 24h-window aggregation. The returned
// snapshot may be nil before the first compute lands; callers must tolerate it.
func getOrComputeDegradationOverview() *DegradationOverviewResponse {
	overviewCacheMutex.RLock()
	cached := cachedOverview
	fresh := cached != nil && time.Since(cachedAt) < overviewCacheTTL
	overviewCacheMutex.RUnlock()
	if fresh {
		return cached
	}

	if overviewRefreshing.CompareAndSwap(false, true) {
		go func() {
			defer overviewRefreshing.Store(false)
			RefreshDegradationOverviewCache()
		}()
	}
	return cached
}

func computeDegradationOverview() *DegradationOverviewResponse {
	setting := degradation_setting.GetDegradationSetting()
	now := time.Now()

	nextRunSecs := 0
	aggregationError := false
	if setting.Enabled {
		latestTasks, err := model.GetLatestScheduledSystemTasks([]string{model.SystemTaskTypeDegradationCheck})
		if err != nil {
			common.SysError(fmt.Sprintf("failed to load degradation schedule for overview: %v", err))
			aggregationError = true
		} else if latest := latestTasks[model.SystemTaskTypeDegradationCheck]; latest != nil &&
			latest.Status != model.SystemTaskStatusPending && latest.Status != model.SystemTaskStatusRunning {
			intervalMinutes := setting.IntervalMinutes
			if intervalMinutes <= 0 {
				intervalMinutes = degradation_setting.DefaultIntervalMinutes
			}
			nextRunSecs = max(0, int(latest.UpdatedAt+int64(intervalMinutes)*60-now.Unix()))
		}
	}

	if len(setting.Targets) == 0 {
		return &DegradationOverviewResponse{
			Stats: DegradationOverviewStats{
				PassRate:         100.0,
				NextRunSeconds:   nextRunSecs,
				Enabled:          setting.Enabled,
				AggregationError: aggregationError,
			},
			Channels: []DegradationChannelGroup{},
		}
	}

	since24h := now.Add(-24 * time.Hour)

	type key struct {
		channelID int
		modelName string
		probeID   string
	}

	// targetStat accumulates SQL-aggregated counts for one probe target.
	type targetStat struct {
		total      int64
		passed     int64
		mismatched int64
		failed     int64 // error + timeout
		latencySum int64
	}

	channelsByID := make(map[int]*model.Channel, len(setting.Targets))
	if len(setting.Targets) > 0 {
		channelIDs := make([]int, 0, len(setting.Targets))
		for _, target := range setting.Targets {
			channelIDs = append(channelIDs, target.ChannelID)
		}
		channels, err := model.GetChannelsByIds(channelIDs)
		if err != nil {
			common.SysError(fmt.Sprintf("failed to load degradation target channels: %v", err))
			aggregationError = true
		} else {
			for _, ch := range channels {
				channelsByID[ch.Id] = ch
			}
		}
	}

	statRows, err := model.GetDegradationLogStatsSince(since24h)
	if err != nil {
		common.SysError(fmt.Sprintf("failed to aggregate degradation log stats for overview: %v", err))
		statRows = []model.DegradationLogStatRow{}
		aggregationError = true
	}

	statsByKey := make(map[key]*targetStat)
	for _, row := range statRows {
		k := key{channelID: row.ChannelId, modelName: row.ModelName, probeID: row.ProbeId}
		st := statsByKey[k]
		if st == nil {
			st = &targetStat{}
			statsByKey[k] = st
		}
		st.total += row.LogCount
		st.latencySum += row.LatencySum
		switch row.Status {
		case model.DegradationStatusPassed:
			st.passed += row.LogCount
		case model.DegradationStatusMismatched:
			st.mismatched += row.LogCount
		case model.DegradationStatusError, model.DegradationStatusTimeout:
			st.failed += row.LogCount
		}
	}

	latestLogs, latestErr := model.GetDegradationLatestLogsSince(since24h)
	latestLogsByKey := make(map[key]model.DegradationCheckLog)
	if latestErr != nil {
		common.SysError(fmt.Sprintf("failed to load latest degradation logs for overview: %v", latestErr))
		aggregationError = true
	} else {
		for _, l := range latestLogs {
			latestLogsByKey[key{channelID: l.ChannelId, modelName: l.ModelName, probeID: l.ProbeId}] = l
		}
	}

	// 24h slots need per-row timestamps, so they are streamed in batches and
	// folded into a fixed-size grid instead of materializing the whole window.
	slotsByTarget := make(map[key][24]int)
	if err := model.FoldDegradationSlotRowsSince(since24h, 1000, func(row model.DegradationSlotRow) {
		k := key{channelID: row.ChannelId, modelName: row.ModelName, probeID: row.ProbeId}
		slots := slotsByTarget[k]
		applySlotRow(&slots, row.Status, row.CreatedAt, now)
		slotsByTarget[k] = slots
	}); err != nil {
		common.SysError(fmt.Sprintf("failed to fold degradation slots for overview: %v", err))
		aggregationError = true
	}

	activeTargetsCount := 0
	// Top-level figures are folded in while walking the displayed targets, so
	// history from channels or models removed from the probe config never
	// skews them (the SQL aggregation intentionally spans all log rows).
	var totalExecs, passedCount, mismatchCount, errorCount int64
	channelGroups := make([]DegradationChannelGroup, 0, len(setting.Targets))

	probeMap := make(map[string]degradation_setting.ProbeConfig, len(setting.Probes))
	for _, p := range setting.Probes {
		probeMap[p.ID] = p
	}

	for _, target := range setting.Targets {
		channel := channelsByID[target.ChannelID]
		if channel == nil {
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

		modelItems := make([]DegradationModelItem, 0, len(models))
		for _, m := range models {
			binding, hasBinding := bindingsMap[m]
			isEnabled := channel.Status == common.ChannelStatusEnabled
			if hasBinding {
				isEnabled = isEnabled && binding.Enabled
			}
			if isEnabled {
				activeTargetsCount++
			}

			var bindingPtr *degradation_setting.ModelProbeBinding
			if hasBinding {
				bindingPtr = &binding
			}
			applicableProbes := applicableProbesForBinding(setting, probeMap, bindingPtr)

			probeTimelines := make([]DegradationProbeTimeline, 0, len(applicableProbes))
			var modelAgg targetStat

			for _, p := range applicableProbes {
				k := key{channelID: target.ChannelID, modelName: m, probeID: p.ID}
				pStat := statsByKey[k]
				if pStat != nil {
					modelAgg.total += pStat.total
					modelAgg.passed += pStat.passed
					modelAgg.latencySum += pStat.latencySum
					// 已暂停的模型不再巡检，其残留的窗口内历史只保留在模型
					// 维度里，不计入全局口径（与前端隐藏暂停模型的展示一致）。
					if isEnabled {
						totalExecs += pStat.total
						passedCount += pStat.passed
						mismatchCount += pStat.mismatched
						errorCount += pStat.failed
					}
				}

				pPassRate := 100.0
				if pStat != nil && pStat.total > 0 {
					pPassRate = float64(pStat.passed) / float64(pStat.total) * 100.0
				}

				var slots [24]int
				if s, ok := slotsByTarget[k]; ok {
					slots = s
				}

				probeTimelines = append(probeTimelines, DegradationProbeTimeline{
					ProbeID:   p.ID,
					ProbeName: p.Name,
					PassRate:  pPassRate,
					Slots:     slots[:],
				})
			}

			mPassRate := 100.0
			avgLatency := 0
			if modelAgg.total > 0 {
				mPassRate = float64(modelAgg.passed) / float64(modelAgg.total) * 100.0
				avgLatency = int(modelAgg.latencySum / modelAgg.total)
			}

			var latestLog *model.DegradationCheckLog
			for _, p := range applicableProbes {
				if ll, ok := latestLogsByKey[key{channelID: target.ChannelID, modelName: m, probeID: p.ID}]; ok {
					if latestLog == nil || ll.ID > latestLog.ID {
						llCopy := ll
						latestLog = &llCopy
					}
				}
			}

			var latestSnapshot *DegradationSnapshotItem
			if latestLog != nil {
				latestSnapshot = &DegradationSnapshotItem{
					Time:            latestLog.CreatedAt.Format(time.RFC3339),
					Status:          latestLog.Status,
					ProbeName:       latestLog.ProbeName,
					LatencyMs:       latestLog.LatencyMs,
					TokenCount:      latestLog.TokenCount,
					ReasoningTokens: latestLog.ReasoningTokens,
					ReasoningEffort: binding.ReasoningEffort,
					Detail:          latestLog.Detail,
				}
			}

			modelItems = append(modelItems, DegradationModelItem{
				ModelName:       m,
				Enabled:         isEnabled,
				ReasoningEffort: binding.ReasoningEffort,
				PassRate:        mPassRate,
				TotalExecutions: modelAgg.total,
				AvgLatencyMs:    avgLatency,
				Probes:          probeTimelines,
				LatestSnapshot:  latestSnapshot,
			})
		}

		channelGroups = append(channelGroups, DegradationChannelGroup{
			ChannelID:   target.ChannelID,
			ChannelName: configuredDegradationChannelName(target, channel),
			Models:      modelItems,
		})
	}

	passRate := 100.0
	if totalExecs > 0 {
		passRate = float64(passedCount) / float64(totalExecs) * 100.0
	}

	return &DegradationOverviewResponse{
		Stats: DegradationOverviewStats{
			PassRate:         passRate,
			TotalExecutions:  int(totalExecs),
			MismatchCount:    int(mismatchCount),
			ErrorCount:       int(errorCount),
			ActiveTargets:    activeTargetsCount,
			NextRunSeconds:   nextRunSecs,
			Enabled:          setting.Enabled,
			AggregationError: aggregationError,
		},
		Channels: channelGroups,
	}
}

// applySlotRow folds one probe execution into the 24-hour slot grid. Within
// one hour the most severe outcome wins: error/timeout > mismatch > passed.
func applySlotRow(slots *[24]int, status int, createdAt time.Time, now time.Time) {
	diffHours := int(now.Sub(createdAt).Hours())
	if diffHours < 0 || diffHours >= 24 {
		return
	}
	slotIdx := 23 - diffHours
	if slotIdx >= 0 && slotIdx < 24 {
		current := slots[slotIdx]
		if status == model.DegradationStatusError || status == model.DegradationStatusTimeout {
			slots[slotIdx] = status
		} else if status == model.DegradationStatusMismatched &&
			current != model.DegradationStatusError && current != model.DegradationStatusTimeout {
			slots[slotIdx] = status
		} else if current == 0 {
			slots[slotIdx] = status
		}
	}
}

// maskedNodeName derives a stable label using the server's private CryptoSecret,
// so public labels cannot be computed by enumerating channel IDs alone.
func maskedNodeName(channelID int) string {
	digest := common.GenerateHMAC("degradation-node:" + strconv.Itoa(channelID))
	return "Node-" + strings.ToUpper(digest[:12])
}

// GET /api/degradation/overview
func GetDegradationOverview(c *gin.Context) {
	raw := getOrComputeDegradationOverview()
	if raw == nil {
		// First request after boot: the background recompute has not landed
		// yet, serve an empty shell (with the live enabled flag) instead of
		// blocking on the aggregation.
		raw = &DegradationOverviewResponse{}
		raw.Stats.Enabled = degradation_setting.GetDegradationSetting().Enabled
	}
	role := c.GetInt("role")
	canReadDetails := role >= common.RoleAdminUser && authz.Can(c.GetInt("id"), role, authz.ChannelRead)
	setting := degradation_setting.GetDegradationSetting()

	// 深度复制并根据权限与脱敏设置脱敏
	res := DegradationOverviewResponse{
		Stats:    raw.Stats,
		Channels: make([]DegradationChannelGroup, 0, len(raw.Channels)),
	}

	for _, ch := range raw.Channels {
		chCopy := DegradationChannelGroup{
			ChannelID:   ch.ChannelID,
			ChannelName: ch.ChannelName,
			Models:      make([]DegradationModelItem, 0, len(ch.Models)),
		}

		if !canReadDetails && setting.PublicMaskTopology {
			chCopy.ChannelName = maskedNodeName(ch.ChannelID)
			chCopy.ChannelID = 0
		}

		for _, m := range ch.Models {
			mCopy := DegradationModelItem{
				ModelName:       m.ModelName,
				Enabled:         m.Enabled,
				PassRate:        m.PassRate,
				TotalExecutions: m.TotalExecutions,
				AvgLatencyMs:    m.AvgLatencyMs,
				Probes:          append([]DegradationProbeTimeline{}, m.Probes...),
			}

			if canReadDetails {
				mCopy.ReasoningEffort = m.ReasoningEffort
			}

			if m.LatestSnapshot != nil {
				sCopy := *m.LatestSnapshot
				if !canReadDetails {
					sCopy.ReasoningEffort = ""
					sCopy.ReasoningTokens = 0
					sCopy.Detail = ""
				}
				mCopy.LatestSnapshot = &sCopy
			}

			chCopy.Models = append(chCopy.Models, mCopy)
		}
		res.Channels = append(res.Channels, chCopy)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    res,
	})
}

// GET /api/degradation/logs (AdminAuth)
func GetDegradationLogs(c *gin.Context) {
	channelID, _ := strconv.Atoi(c.Query("channel_id"))
	modelName := c.Query("model_name")
	status, _ := strconv.Atoi(c.Query("status"))
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	logs, total, err := model.GetDegradationLogsPaged(channelID, modelName, status, page, pageSize)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items": logs,
			"total": total,
		},
	})
}

// degradationRequestCoveredByActive reports whether an already-active
// degradation task will actually produce the results the caller asked for.
// Full rounds only cover another full-round request: their captured execution
// targets are not persisted, so live settings cannot prove single-target
// coverage. A single-target task covers its own channel/model combination.
func degradationRequestCoveredByActive(active *model.SystemTask, filter degradationTargetFilter) bool {
	var activeFilter degradationTargetFilter
	if err := active.DecodePayload(&activeFilter); err != nil {
		return false
	}
	if activeFilter.ChannelID == 0 {
		return filter.ChannelID == 0
	}
	return activeFilter.ChannelID == filter.ChannelID && activeFilter.ModelName == filter.ModelName
}

// POST /api/degradation/run (AdminAuth) 手动触发一轮
func RunDegradationCheck(c *gin.Context) {
	if !degradation_setting.GetDegradationSetting().Enabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "degradation check is disabled",
		})
		return
	}

	active, created, err := service.EnqueueSystemTask(model.SystemTaskTypeDegradationCheck, nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("failed to enqueue degradation task: %v", err),
		})
		return
	}

	if !created {
		// Tasks of this type dedupe by active row, so the request only
		// succeeds when the running task already covers it. Otherwise it
		// would be silently dropped while claiming to have run.
		if !degradationRequestCoveredByActive(active, degradationTargetFilter{}) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "a single-target degradation check is already running, retry after it finishes",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "degradation check task already active, request merged into the running round",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "degradation check task enqueued",
	})
}

// POST /api/degradation/run-target (AdminAuth) 手动单测某个目标（入队异步执行，结果写入巡检日志）
func RunTargetDegradationCheck(c *gin.Context) {
	var req struct {
		ChannelID int    `json:"channel_id"`
		ModelName string `json:"model_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid request body"})
		return
	}
	if req.ChannelID <= 0 || req.ModelName == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel_id and model_name are required"})
		return
	}

	setting := degradation_setting.GetDegradationSetting()
	if !setting.Enabled {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "degradation check is disabled"})
		return
	}
	if len(setting.Probes) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "no probes configured"})
		return
	}

	if channel, err := model.GetChannelById(req.ChannelID, true); err != nil || channel == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel not found"})
		return
	}

	active, created, err := service.EnqueueSystemTask(model.SystemTaskTypeDegradationCheck, degradationTargetFilter{
		ChannelID: req.ChannelID,
		ModelName: req.ModelName,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("failed to enqueue degradation task: %v", err),
		})
		return
	}

	if !created {
		if !degradationRequestCoveredByActive(active, degradationTargetFilter{
			ChannelID: req.ChannelID,
			ModelName: req.ModelName,
		}) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "another degradation check task is already running, retry after it finishes",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "degradation check task already active, request merged into the running round",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "target probe task enqueued, results will be written to check logs",
	})
}

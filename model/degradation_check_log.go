package model

import (
	"time"
)

const (
	DegradationStatusPassed     = 1 // 检测通过
	DegradationStatusMismatched = 2 // 答案不符 / 降智
	DegradationStatusError      = 3 // 上游请求错误
	DegradationStatusTimeout    = 4 // 请求超时
)

type DegradationCheckLog struct {
	ID              int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	CreatedAt       time.Time `json:"created_at" gorm:"index:idx_degradation_created_at"`
	ChannelId       int       `json:"channel_id" gorm:"index:idx_degradation_channel"`
	ChannelName     string    `json:"channel_name" gorm:"type:varchar(128)"`
	ModelName       string    `json:"model_name" gorm:"type:text"`
	ProbeId         string    `json:"probe_id" gorm:"type:varchar(64)"`
	ProbeName       string    `json:"probe_name" gorm:"type:varchar(128)"`
	Status          int       `json:"status" gorm:"type:smallint;index:idx_degradation_status"`
	LatencyMs       int       `json:"latency_ms"`
	TokenCount      int       `json:"token_count"`
	ReasoningTokens int       `json:"reasoning_tokens"`
	Detail          string    `json:"detail" gorm:"type:varchar(512)"`
}

// 列宽上限与 gorm 标签的 varchar 长度（字符数）对齐。超宽写入在 MySQL 严格
// 模式与 PostgreSQL 会整批报错丢数据，SQLite 又不校验长度，因此入库前统一按
// rune 截断展示字段兜底；模型名是查询与聚合的标识，必须完整保存。
const (
	degradationChannelNameLen = 128
	degradationProbeIdLen     = 64
	degradationProbeNameLen   = 128
	degradationDetailLen      = 512
)

func truncateRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

func normalizeDegradationLog(log *DegradationCheckLog) {
	log.ChannelName = truncateRunes(log.ChannelName, degradationChannelNameLen)
	log.ProbeId = truncateRunes(log.ProbeId, degradationProbeIdLen)
	log.ProbeName = truncateRunes(log.ProbeName, degradationProbeNameLen)
	log.Detail = truncateRunes(log.Detail, degradationDetailLen)
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now()
	}
}

func RecordDegradationLog(log *DegradationCheckLog) error {
	normalizeDegradationLog(log)
	return DB.Create(log).Error
}

// RecordBatchDegradationLogs 分片写入并返回实际持久化的行数；分片中途失败时
// 已写入的分片不会回滚，调用方需要用返回值区分"执行了"与"落库了"。
func RecordBatchDegradationLogs(logs []*DegradationCheckLog) (int64, error) {
	if len(logs) == 0 {
		return 0, nil
	}
	for _, l := range logs {
		normalizeDegradationLog(l)
	}
	// 分片写入，避免大轮次超出 SQLite 等驱动的绑定参数上限
	const batchSize = 200
	var persisted int64
	for start := 0; start < len(logs); start += batchSize {
		end := start + batchSize
		if end > len(logs) {
			end = len(logs)
		}
		batch := logs[start:end]
		if err := DB.Create(&batch).Error; err != nil {
			return persisted, err
		}
		persisted += int64(len(batch))
	}
	return persisted, nil
}

// DegradationLogStatRow is one (channel_id, model_name, probe_id, status)
// group produced by SQL aggregation. Keeping per-status counts and latency
// sums in SQL bounds the overview query to targets × 4 rows instead of every
// execution row in the 24h window.
type DegradationLogStatRow struct {
	ChannelId  int
	ModelName  string
	ProbeId    string
	Status     int
	LogCount   int64
	LatencySum int64
}

func degradationCaseSensitiveColumn(column string) string {
	if DB != nil && DB.Dialector.Name() == "mysql" {
		return "BINARY " + column
	}
	return column
}

func GetDegradationLogStatsSince(since time.Time) ([]DegradationLogStatRow, error) {
	var rows []DegradationLogStatRow
	modelColumn := degradationCaseSensitiveColumn("model_name")
	probeColumn := degradationCaseSensitiveColumn("probe_id")
	modelSelect := "model_name"
	probeSelect := "probe_id"
	if DB != nil && DB.Dialector.Name() == "mysql" {
		// ONLY_FULL_GROUP_BY requires selected expressions to match their
		// grouped expressions; otherwise selecting the uncast column beside
		// GROUP BY BINARY model_name/probe_id fails with MySQL error 1055.
		modelSelect = "BINARY model_name AS model_name"
		probeSelect = "BINARY probe_id AS probe_id"
	}
	err := DB.Model(&DegradationCheckLog{}).
		Select("channel_id", modelSelect, probeSelect, "status", "COUNT(*) as log_count", "SUM(latency_ms) as latency_sum").
		Where("created_at >= ?", since).
		Group("channel_id, " + modelColumn + ", " + probeColumn + ", status").
		Scan(&rows).Error
	return rows, err
}

// DegradationSlotRow carries the minimal columns needed to fold one probe
// execution into its 24-hour overview slot.
type DegradationSlotRow struct {
	ID        int64
	ChannelId int
	ModelName string
	ProbeId   string
	Status    int
	CreatedAt time.Time
}

// FoldDegradationSlotRowsSince streams the window in fixed-size primary-key
// ordered batches and invokes fn per row, so slot computation never
// materializes the whole window in memory. Pagination uses keyset pagination
// by primary key (id > lastID order by id asc limit batchSize) without
// extra roundtrips or large IN clause parameters.
func FoldDegradationSlotRowsSince(since time.Time, batchSize int, fn func(DegradationSlotRow)) error {
	if batchSize < 1 {
		batchSize = 1000
	}
	for lastID := int64(0); ; {
		var batch []DegradationSlotRow
		tx := DB.Model(&DegradationCheckLog{}).
			Select("id", "channel_id", "model_name", "probe_id", "status", "created_at").
			Where("created_at >= ?", since).
			Order("id ASC").
			Limit(batchSize)
		if lastID > 0 {
			tx = tx.Where("id > ?", lastID)
		}
		if err := tx.Scan(&batch).Error; err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, row := range batch {
			fn(row)
		}
		lastID = batch[len(batch)-1].ID
		if len(batch) < batchSize {
			return nil
		}
	}
}

// GetDegradationLatestLogsSince returns the most recent full log row per
// (channel_id, model_name, probe_id) within the window. MAX(id) with GROUP BY
// stays compatible with MySQL 5.7, PostgreSQL and SQLite alike.
func GetDegradationLatestLogsSince(since time.Time) ([]DegradationCheckLog, error) {
	modelColumn := degradationCaseSensitiveColumn("model_name")
	probeColumn := degradationCaseSensitiveColumn("probe_id")
	latestIDs := DB.Model(&DegradationCheckLog{}).
		Select("MAX(id)").
		Where("created_at >= ?", since).
		Group("channel_id, " + modelColumn + ", " + probeColumn)

	var logs []DegradationCheckLog
	err := DB.Where("created_at >= ?", since).
		Where("id IN (?)", latestIDs).
		Find(&logs).Error
	return logs, err
}

func GetDegradationLogsPaged(channelId int, modelName string, status int, page int, pageSize int) ([]*DegradationCheckLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	tx := DB.Model(&DegradationCheckLog{})
	if channelId > 0 {
		tx = tx.Where("channel_id = ?", channelId)
	}
	if modelName != "" {
		tx = tx.Where(degradationCaseSensitiveColumn("model_name")+" = ?", modelName)
	}
	if status > 0 {
		tx = tx.Where("status = ?", status)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var logs []*DegradationCheckLog
	err := tx.Order("id desc").
		Limit(pageSize).
		Offset(offset).
		Find(&logs).Error

	return logs, total, err
}

// DeleteExpiredDegradationLogs batches by primary key: SELECT ... LIMIT works
// on all three supported databases, while DELETE ... LIMIT silently loses its
// LIMIT clause on PostgreSQL and SQLite.
func DeleteExpiredDegradationLogs(expireTime time.Time) (int64, error) {
	var totalDeleted int64
	for {
		var ids []int64
		if err := DB.Model(&DegradationCheckLog{}).
			Where("created_at < ?", expireTime).
			Order("id ASC").
			Limit(1000).
			Pluck("id", &ids).Error; err != nil {
			return totalDeleted, err
		}
		if len(ids) == 0 {
			break
		}
		res := DB.Where("id IN ?", ids).Delete(&DegradationCheckLog{})
		if res.Error != nil {
			return totalDeleted, res.Error
		}
		totalDeleted += res.RowsAffected
		if len(ids) < 1000 {
			break
		}
	}
	return totalDeleted, nil
}

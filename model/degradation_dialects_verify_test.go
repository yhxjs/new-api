package model

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// TestDegradationCheckLogDialectVerification exercises the degradation log
// schema and query paths against a real MySQL or PostgreSQL instance. It is
// gated behind environment variables so the default SQLite test run is
// unaffected:
//
//	DEGRADATION_DIALECT_DB=mysql|postgres
//	DEGRADATION_DIALECT_DSN=<driver DSN>
//
// AutoMigrate runs twice on a fresh database to prove idempotency.
func TestDegradationCheckLogDialectVerification(t *testing.T) {
	dialect := os.Getenv("DEGRADATION_DIALECT_DB")
	dsn := os.Getenv("DEGRADATION_DIALECT_DSN")
	if dialect == "" || dsn == "" {
		t.Skip("DEGRADATION_DIALECT_DB/DEGRADATION_DIALECT_DSN not set")
	}

	db := openDegradationVerificationDB(t, dialect, dsn)

	origDB, origLogDB := DB, LOG_DB
	DB, LOG_DB = db, db
	defer func() { DB, LOG_DB = origDB, origLogDB }()

	// Run the same migration used by startup twice, including old-index removal.
	require.NoError(t, migrateDegradationCheckLogs())
	require.NoError(t, migrateDegradationCheckLogs())

	require.NoError(t, db.Where("1 = 1").Delete(&DegradationCheckLog{}).Error)

	// Display fields are bounded; the model identifier must be preserved.
	long := strings.Repeat("渠道", 1500) // Also exceeds PostgreSQL's btree entry limit.
	single := &DegradationCheckLog{
		ChannelId:   99001,
		ChannelName: long,
		ModelName:   long,
		ProbeId:     strings.Repeat("p", 80),
		ProbeName:   long,
		Status:      DegradationStatusPassed,
		LatencyMs:   100,
		CreatedAt:   time.Now().Add(-2 * time.Hour),
	}
	require.NoError(t, RecordDegradationLog(single))
	require.NotZero(t, single.ID)

	var stored DegradationCheckLog
	require.NoError(t, db.Where("channel_id = ?", 99001).First(&stored).Error)
	assert.Len(t, []rune(stored.ChannelName), 128)
	assert.Equal(t, long, stored.ModelName)
	matched, matchedTotal, err := GetDegradationLogsPaged(99001, long, 0, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), matchedTotal)
	assert.Len(t, matched, 1)
	assert.Len(t, []rune(stored.ProbeId), 64)
	assert.Len(t, []rune(stored.ProbeName), 128)

	// 批量写入 + 窗口聚合 + 最新一条 + 分页 + 过期清理
	base := time.Now().Add(-time.Hour)
	batch := []*DegradationCheckLog{
		{ChannelId: 99002, ModelName: "m1", ProbeId: "p1", Status: DegradationStatusPassed, LatencyMs: 100, CreatedAt: base},
		{ChannelId: 99002, ModelName: "m1", ProbeId: "p1", Status: DegradationStatusPassed, LatencyMs: 300, CreatedAt: base},
		{ChannelId: 99002, ModelName: "m1", ProbeId: "p1", Status: DegradationStatusMismatched, LatencyMs: 400, CreatedAt: base},
		{ChannelId: 99002, ModelName: "m1", ProbeId: "p2", Status: DegradationStatusTimeout, LatencyMs: 5000, CreatedAt: base},
		// 窗口外，聚合与清理场景都要排除/命中
		{ChannelId: 99002, ModelName: "m1", ProbeId: "p1", Status: DegradationStatusError, LatencyMs: 9999, CreatedAt: base.Add(-25 * time.Hour)},
	}
	persisted, err := RecordBatchDegradationLogs(batch)
	require.NoError(t, err)
	assert.Equal(t, int64(len(batch)), persisted)

	stats, err := GetDegradationLogStatsSince(base.Add(-time.Minute))
	require.NoError(t, err)
	statCount := func(status int) (int64, int64) {
		for _, row := range stats {
			if row.ChannelId == 99002 && row.ModelName == "m1" && row.ProbeId == "p1" && row.Status == status {
				return row.LogCount, row.LatencySum
			}
		}
		return 0, 0
	}
	count, sum := statCount(DegradationStatusPassed)
	assert.Equal(t, int64(2), count)
	assert.Equal(t, int64(400), sum)
	count, _ = statCount(DegradationStatusMismatched)
	assert.Equal(t, int64(1), count)
	// 窗口外 error 行不得进入聚合
	count, _ = statCount(DegradationStatusError)
	assert.Equal(t, int64(0), count)

	latest, err := GetDegradationLatestLogsSince(base.Add(-time.Minute))
	require.NoError(t, err)
	foundLatest := false
	for _, l := range latest {
		if l.ChannelId == 99002 && l.ModelName == "m1" && l.ProbeId == "p1" {
			foundLatest = true
			// 同 key 内最后写入的一行（Mismatched，400ms）
			assert.Equal(t, DegradationStatusMismatched, l.Status)
		}
	}
	assert.True(t, foundLatest)

	logs, total, err := GetDegradationLogsPaged(99002, "m1", 0, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total) // 含窗口外 1 行：分页不做时间过滤
	assert.Len(t, logs, 5)

	var folded []DegradationSlotRow
	require.NoError(t, FoldDegradationSlotRowsSince(base.Add(-time.Minute), 2, func(row DegradationSlotRow) {
		folded = append(folded, row)
	}))
	assert.Len(t, folded, 4) // batchSize=2 强制多轮翻页

	deleted, err := DeleteExpiredDegradationLogs(base.Add(-24 * time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	var remain int64
	require.NoError(t, db.Model(&DegradationCheckLog{}).Where("channel_id IN ?", []int{99001, 99002}).Count(&remain).Error)
	assert.Equal(t, int64(5), remain)

	require.NoError(t, db.Where("channel_id IN ?", []int{99001, 99002}).Delete(&DegradationCheckLog{}).Error)
}

func TestSystemTaskScheduledQueryDialectVerification(t *testing.T) {
	dialect := os.Getenv("DEGRADATION_DIALECT_DB")
	dsn := os.Getenv("DEGRADATION_DIALECT_DSN")
	if dialect == "" || dsn == "" {
		t.Skip("DEGRADATION_DIALECT_DB/DEGRADATION_DIALECT_DSN not set")
	}

	db := openDegradationVerificationDB(t, dialect, dsn)
	originalDB := DB
	DB = db
	t.Cleanup(func() { DB = originalDB })
	require.NoError(t, db.Migrator().DropTable(&SystemTaskLock{}, &SystemTask{}))
	require.NoError(t, db.AutoMigrate(&SystemTask{}, &SystemTaskLock{}))

	manual, err := CreateSystemTask(SystemTaskTypeDegradationCheck, map[string]any{"channel_id": 7}, nil)
	require.NoError(t, err)
	_, claimed, err := ClaimSystemTask(manual.ID, manual.Type, "dialect-manual", time.Now().Unix()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, FinishSystemTask(manual.TaskID, "dialect-manual", SystemTaskStatusSucceeded, nil, ""))

	latest, err := GetLatestScheduledSystemTasks([]string{SystemTaskTypeDegradationCheck})
	require.NoError(t, err)
	assert.Nil(t, latest[SystemTaskTypeDegradationCheck])

	scheduled, err := CreateSystemTask(SystemTaskTypeDegradationCheck, nil, nil)
	require.NoError(t, err)
	_, claimed, err = ClaimSystemTask(scheduled.ID, scheduled.Type, "dialect-scheduled", time.Now().Unix()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, FinishSystemTask(scheduled.TaskID, "dialect-scheduled", SystemTaskStatusSucceeded, nil, ""))
	latest, err = GetLatestScheduledSystemTasks([]string{SystemTaskTypeDegradationCheck})
	require.NoError(t, err)
	require.Equal(t, scheduled.TaskID, latest[SystemTaskTypeDegradationCheck].TaskID)

	for _, taskType := range []string{SystemTaskTypeChannelTest, SystemTaskTypeModelUpdate} {
		manual, err := CreateSystemTask(taskType, map[string]any{"manual": true}, nil)
		require.NoError(t, err)
		_, claimed, err := ClaimSystemTask(manual.ID, taskType, "dialect-other-manual", time.Now().Unix()+60)
		require.NoError(t, err)
		require.True(t, claimed)
		require.NoError(t, FinishSystemTask(manual.TaskID, "dialect-other-manual", SystemTaskStatusSucceeded, nil, ""))
		latest, err := GetLatestSystemTasks([]string{taskType})
		require.NoError(t, err)
		require.Equal(t, manual.TaskID, latest[taskType].TaskID)
	}

	manualPending, err := CreateSystemTask(SystemTaskTypeDegradationCheck, map[string]any{"channel_id": 8}, nil)
	require.NoError(t, err)
	latest, err = GetLatestScheduledSystemTasks([]string{SystemTaskTypeDegradationCheck})
	require.NoError(t, err)
	require.Equal(t, manualPending.TaskID, latest[SystemTaskTypeDegradationCheck].TaskID)
}

func openDegradationVerificationDB(t *testing.T, dialect, dsn string) *gorm.DB {
	t.Helper()
	var dialector gorm.Dialector
	switch dialect {
	case "mysql":
		dialector = mysql.Open(dsn)
	case "postgres":
		dialector = postgres.Open(dsn)
	case "sqlite":
		dialector = sqlite.Open(dsn)
	default:
		t.Fatalf("unsupported dialect %q", dialect)
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	if dialect == "sqlite" {
		var version string
		require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
		t.Logf("SQLite %s", version)
	}
	return db
}

// The unreleased narrow schema must also upgrade without losing recorded data.
type legacyDegradationCheckLog struct {
	ID              int64     `gorm:"primaryKey;autoIncrement"`
	CreatedAt       time.Time `gorm:"index:idx_degradation_created_at"`
	ChannelId       int       `gorm:"index:idx_degradation_channel_model,priority:1"`
	ChannelName     string    `gorm:"type:varchar(128)"`
	ModelName       string    `gorm:"type:varchar(128);index:idx_degradation_channel_model,priority:2"`
	ProbeId         string    `gorm:"type:varchar(64)"`
	ProbeName       string    `gorm:"type:varchar(128)"`
	Status          int       `gorm:"type:smallint;index:idx_degradation_status"`
	LatencyMs       int
	TokenCount      int
	ReasoningTokens int
	Detail          string `gorm:"type:varchar(512)"`
}

func (legacyDegradationCheckLog) TableName() string { return "degradation_check_logs" }

func TestDegradationCheckLogMigration(t *testing.T) {
	dialect, dsn := os.Getenv("DEGRADATION_DIALECT_DB"), os.Getenv("DEGRADATION_DIALECT_DSN")
	if dialect == "" && dsn == "" {
		dialect, dsn = "sqlite", ":memory:"
	}
	db := openDegradationVerificationDB(t, dialect, dsn)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
	var releasedUsers, releasedOptions []map[string]any
	var releasedTables []string
	if os.Getenv("DEGRADATION_RELEASE_BASELINE") == "1" {
		// This mode is only for an isolated database initialized by the release
		// image. Seed representative data through its real existing schema.
		require.True(t, db.Migrator().HasTable("users"))
		cleanupFixture := func() {
			assert.NoError(t, db.Table("users").Where("username = ?", "review-admin").Delete(nil).Error)
			assert.NoError(t, db.Table("options").Where(map[string]any{"key": "degradation-review-fixture"}).Delete(nil).Error)
		}
		cleanupFixture()
		t.Cleanup(cleanupFixture)
		require.NoError(t, db.Table("users").Create(map[string]any{
			"username": "review-admin", "password": "isolated-migration-fixture", "role": 100,
			"status": 1, "quota": 123456, "used_quota": 123,
		}).Error)
		require.NoError(t, db.Table("options").Create(map[string]any{"key": "degradation-review-fixture", "value": "preserved-setting"}).Error)
		require.NoError(t, db.Table("users").Order("id").Find(&releasedUsers).Error)
		require.NoError(t, db.Table("options").Order(clause.OrderByColumn{Column: clause.Column{Name: "key"}}).Find(&releasedOptions).Error)
		var err error
		releasedTables, err = db.Migrator().GetTables()
		require.NoError(t, err)
		assert.True(t, db.Migrator().HasIndex("users", "idx_users_username"))
	}
	for _, legacy := range []bool{false, true} {
		name := "fresh"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			require.NoError(t, db.Migrator().DropTable(&DegradationCheckLog{}))
			preserved := legacyDegradationCheckLog{ChannelId: 7, ModelName: "preserve-model", ProbeId: "p1", Status: DegradationStatusMismatched, Detail: "preserved-result", CreatedAt: time.Now().Truncate(time.Second)}
			if legacy {
				require.NoError(t, db.AutoMigrate(&legacyDegradationCheckLog{}))
				require.NoError(t, db.Create(&preserved).Error)
			}
			for range 2 {
				require.NoError(t, migrateDegradationCheckLogs())
			}
			assert.True(t, db.Migrator().HasIndex(&DegradationCheckLog{}, "idx_degradation_created_at"))
			assert.True(t, db.Migrator().HasIndex(&DegradationCheckLog{}, "idx_degradation_status"))
			assert.True(t, db.Migrator().HasIndex(&DegradationCheckLog{}, "idx_degradation_channel"))
			assert.False(t, db.Migrator().HasIndex(&DegradationCheckLog{}, "idx_degradation_channel_model"))
			if legacy {
				var row DegradationCheckLog
				require.NoError(t, db.First(&row, preserved.ID).Error)
				assert.Equal(t, preserved.ModelName, row.ModelName)
				assert.Equal(t, preserved.Detail, row.Detail)
				assert.Equal(t, preserved.CreatedAt.Unix(), row.CreatedAt.Unix())
				assert.Equal(t, preserved.Status, row.Status)
				assert.Error(t, db.Create(&row).Error, "primary key uniqueness must remain enforced")
			}
			longName := strings.Repeat("model", 600)
			row := &DegradationCheckLog{ChannelId: 7, ModelName: longName, ProbeId: "p1", Status: DegradationStatusError}
			require.NoError(t, RecordDegradationLog(row))
			logs, total, err := GetDegradationLogsPaged(7, longName, 0, 1, 10)
			require.NoError(t, err)
			require.Len(t, logs, 1)
			assert.Equal(t, int64(1), total)
			assert.Equal(t, longName, logs[0].ModelName)
			if legacy {
				assert.Greater(t, row.ID, preserved.ID)
			}
			if releasedTables != nil {
				var users, options []map[string]any
				require.NoError(t, db.Table("users").Order("id").Find(&users).Error)
				require.NoError(t, db.Table("options").Order(clause.OrderByColumn{Column: clause.Column{Name: "key"}}).Find(&options).Error)
				assert.Equal(t, releasedUsers, users)
				assert.Equal(t, releasedOptions, options)
				for _, table := range releasedTables {
					assert.True(t, db.Migrator().HasTable(table), "released table %s must remain", table)
				}
				assert.True(t, db.Migrator().HasIndex("users", "idx_users_username"))
				duplicate := map[string]any{"username": "review-admin", "password": "duplicate-migration-fixture"}
				assert.Error(t, db.Table("users").Create(duplicate).Error, "released username uniqueness must remain enforced")
			}
		})
	}
	require.NoError(t, db.Migrator().DropTable(&DegradationCheckLog{}))
}

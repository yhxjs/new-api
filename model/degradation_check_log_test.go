package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDegradationCheckLogCRUD(t *testing.T) {
	if DB == nil {
		t.Skip("DB not initialized in test environment")
	}

	_ = DB.AutoMigrate(&DegradationCheckLog{})

	log := &DegradationCheckLog{
		ChannelId:       999,
		ChannelName:     "Test Channel",
		ModelName:       "test-model",
		ProbeId:         "probe_test",
		ProbeName:       "Test Probe",
		Status:          DegradationStatusPassed,
		LatencyMs:       150,
		TokenCount:      20,
		ReasoningTokens: 0,
		Detail:          "",
		CreatedAt:       time.Now(),
	}

	err := RecordDegradationLog(log)
	require.NoError(t, err)
	assert.Positive(t, log.ID)

	logs, total, err := GetDegradationLogsPaged(999, "test-model", 0, 1, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, int64(1))
	assert.NotEmpty(t, logs)

	// Clean up
	DB.Delete(log)
}

func TestDegradationLogColumnTruncation(t *testing.T) {
	if DB == nil {
		t.Skip("DB not initialized in test environment")
	}

	_ = DB.AutoMigrate(&DegradationCheckLog{})
	long := strings.Repeat("渠道", 100)       // 200 rune，超出 varchar(128)
	longDetail := strings.Repeat("详情", 300) // 600 rune，超出 varchar(512)
	log := &DegradationCheckLog{
		ChannelId:   995,
		ChannelName: long,
		ModelName:   long,
		ProbeId:     strings.Repeat("p", 80),
		ProbeName:   long,
		Detail:      longDetail,
		Status:      DegradationStatusPassed,
	}

	require.NoError(t, RecordDegradationLog(log))

	var stored DegradationCheckLog
	require.NoError(t, DB.Where("channel_id = ?", 995).First(&stored).Error)
	assert.Len(t, []rune(stored.ChannelName), 128)
	assert.Equal(t, long, stored.ModelName)
	logs, total, err := GetDegradationLogsPaged(995, long, 0, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, logs, 1)
	assert.Len(t, []rune(stored.ProbeId), 64)
	assert.Len(t, []rune(stored.ProbeName), 128)
	assert.Len(t, []rune(stored.Detail), 512)

	// Clean up
	DB.Where("channel_id = ?", 995).Delete(&DegradationCheckLog{})
}

func TestGetDegradationLatestLogsSince(t *testing.T) {
	if DB == nil {
		t.Skip("DB not initialized in test environment")
	}

	_ = DB.AutoMigrate(&DegradationCheckLog{})
	base := time.Now().Add(-time.Hour)

	rows := []DegradationCheckLog{
		{ChannelId: 991, ModelName: "m1", ProbeId: "p1", ProbeName: "old", Detail: "old-run", Status: DegradationStatusPassed, CreatedAt: base},
		{ChannelId: 991, ModelName: "m1", ProbeId: "p1", ProbeName: "mid", Detail: "mid-run", Status: DegradationStatusMismatched, CreatedAt: base.Add(time.Minute)},
		{ChannelId: 991, ModelName: "m1", ProbeId: "p1", ProbeName: "new", Detail: "new-run", Status: DegradationStatusError, CreatedAt: base.Add(2 * time.Minute)},
		{ChannelId: 991, ModelName: "m1", ProbeId: "p2", ProbeName: "other-probe", Detail: "p2-run", Status: DegradationStatusPassed, CreatedAt: base},
		{ChannelId: 992, ModelName: "m2", ProbeId: "p1", ProbeName: "second-channel", Detail: "ch2-run", Status: DegradationStatusPassed, CreatedAt: base},
		{ChannelId: 992, ModelName: "m2", ProbeId: "p1", ProbeName: "expired", Detail: "expired-run", Status: DegradationStatusError, CreatedAt: base.Add(-25 * time.Hour)},
	}
	require.NoError(t, DB.Create(&rows).Error)

	logs, err := GetDegradationLatestLogsSince(base.Add(-time.Minute))
	require.NoError(t, err)
	assert.Len(t, logs, 3)

	latestByKey := make(map[string]string)
	for _, l := range logs {
		latestByKey[fmt.Sprintf("%d|%s|%s", l.ChannelId, l.ModelName, l.ProbeId)] = l.Detail
	}
	assert.Equal(t, "new-run", latestByKey["991|m1|p1"])
	assert.Equal(t, "p2-run", latestByKey["991|m1|p2"])
	assert.Equal(t, "ch2-run", latestByKey["992|m2|p1"])

	// Clean up
	DB.Where("channel_id IN ?", []int{991, 992}).Delete(&DegradationCheckLog{})
}

func TestGetDegradationLogStatsSince(t *testing.T) {
	if DB == nil {
		t.Skip("DB not initialized in test environment")
	}

	_ = DB.AutoMigrate(&DegradationCheckLog{})
	base := time.Now().Add(-time.Hour)

	rows := []DegradationCheckLog{
		{ChannelId: 993, ModelName: "m1", ProbeId: "p1", Status: DegradationStatusPassed, LatencyMs: 100, CreatedAt: base},
		{ChannelId: 993, ModelName: "m1", ProbeId: "p1", Status: DegradationStatusPassed, LatencyMs: 300, CreatedAt: base},
		{ChannelId: 993, ModelName: "m1", ProbeId: "p1", Status: DegradationStatusMismatched, LatencyMs: 400, CreatedAt: base},
		{ChannelId: 993, ModelName: "m1", ProbeId: "p2", Status: DegradationStatusTimeout, LatencyMs: 5000, CreatedAt: base},
		// Outside the window must be excluded from aggregation.
		{ChannelId: 993, ModelName: "m1", ProbeId: "p1", Status: DegradationStatusPassed, LatencyMs: 9999, CreatedAt: base.Add(-25 * time.Hour)},
	}
	require.NoError(t, DB.Create(&rows).Error)

	stats, err := GetDegradationLogStatsSince(base.Add(-time.Minute))
	require.NoError(t, err)

	type statKey struct {
		channelID int
		model     string
		probe     string
		status    int
	}
	byKey := make(map[statKey]DegradationLogStatRow)
	for _, s := range stats {
		byKey[statKey{s.ChannelId, s.ModelName, s.ProbeId, s.Status}] = s
	}

	assert.Equal(t, int64(2), byKey[statKey{993, "m1", "p1", DegradationStatusPassed}].LogCount)
	assert.Equal(t, int64(400), byKey[statKey{993, "m1", "p1", DegradationStatusPassed}].LatencySum)
	assert.Equal(t, int64(1), byKey[statKey{993, "m1", "p1", DegradationStatusMismatched}].LogCount)
	assert.Equal(t, int64(1), byKey[statKey{993, "m1", "p2", DegradationStatusTimeout}].LogCount)
	assert.NotContains(t, byKey, statKey{993, "m1", "p1", DegradationStatusError})

	// Clean up
	DB.Where("channel_id = ?", 993).Delete(&DegradationCheckLog{})
}

func TestDegradationLogIdentityMatchingIsCaseSensitive(t *testing.T) {
	if DB == nil {
		t.Skip("DB not initialized in test environment")
	}

	_ = DB.AutoMigrate(&DegradationCheckLog{})
	base := time.Now().Add(-time.Hour)
	rows := []DegradationCheckLog{
		{ChannelId: 996, ModelName: "Foo", ProbeId: "P1", Status: DegradationStatusPassed, LatencyMs: 100, CreatedAt: base},
		{ChannelId: 996, ModelName: "foo", ProbeId: "p1", Status: DegradationStatusError, LatencyMs: 200, CreatedAt: base.Add(time.Minute)},
	}
	require.NoError(t, DB.Create(&rows).Error)

	stats, err := GetDegradationLogStatsSince(base.Add(-time.Minute))
	require.NoError(t, err)
	assert.Len(t, stats, 2)

	latest, err := GetDegradationLatestLogsSince(base.Add(-time.Minute))
	require.NoError(t, err)
	assert.Len(t, latest, 2)

	_, total, err := GetDegradationLogsPaged(996, "Foo", 0, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	_, total, err = GetDegradationLogsPaged(996, "foo", 0, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)

	DB.Where("channel_id = ?", 996).Delete(&DegradationCheckLog{})
}

func TestFoldDegradationSlotRowsSince(t *testing.T) {
	if DB == nil {
		t.Skip("DB not initialized in test environment")
	}

	_ = DB.AutoMigrate(&DegradationCheckLog{})
	base := time.Now().Add(-2 * time.Hour)

	rows := []DegradationCheckLog{
		{ChannelId: 994, ModelName: "m1", ProbeId: "p1", Status: DegradationStatusPassed, CreatedAt: base},
		{ChannelId: 994, ModelName: "m1", ProbeId: "p1", Status: DegradationStatusError, CreatedAt: base},
		{ChannelId: 994, ModelName: "m1", ProbeId: "p1", Status: DegradationStatusTimeout, CreatedAt: base.Add(-25 * time.Hour)},
	}
	require.NoError(t, DB.Create(&rows).Error)

	var folded []DegradationSlotRow
	err := FoldDegradationSlotRowsSince(base.Add(-time.Minute), 1, func(row DegradationSlotRow) {
		folded = append(folded, row)
	})
	require.NoError(t, err)
	assert.Len(t, folded, 2)
	for _, row := range folded {
		assert.Equal(t, 994, row.ChannelId)
		assert.Equal(t, "m1", row.ModelName)
		assert.Equal(t, "p1", row.ProbeId)
	}

	// Clean up
	DB.Where("channel_id = ?", 994).Delete(&DegradationCheckLog{})
}

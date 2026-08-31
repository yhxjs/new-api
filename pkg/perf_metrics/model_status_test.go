package perfmetrics

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQueryStatusMergesDatabaseAndHotBuckets(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}))
	model.DB = db
	hotBuckets = sync.Map{}
	t.Cleanup(func() {
		model.DB = previousDB
		hotBuckets = sync.Map{}
	})

	bucketTs := time.Now().UTC().Truncate(time.Hour).Unix()
	require.NoError(t, db.Create([]model.PerfMetric{
		{
			ModelName:      "gpt-test",
			Group:          "default",
			BucketTs:       bucketTs,
			RequestCount:   2,
			SuccessCount:   1,
			TotalLatencyMs: 2000,
			TtftSumMs:      600,
			TtftCount:      1,
			OutputTokens:   20,
			GenerationMs:   1000,
		},
	}).Error)

	hot := &atomicBucket{}
	hot.add(Sample{
		Model:        "gpt-test",
		Group:        "default",
		LatencyMs:    1000,
		TtftMs:       300,
		HasTtft:      true,
		Success:      true,
		OutputTokens: 10,
		GenerationMs: 1000,
	})
	hotBuckets.Store(bucketKey{model: "gpt-test", group: "default", bucketTs: bucketTs}, hot)

	result, err := QueryStatus(24, nil)

	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	assert.Equal(t, int64(3), result.Models[0].RequestCount)
	assert.Equal(t, int64(2), result.Models[0].SuccessCount)
	require.NotNil(t, result.Models[0].AvgTtftMs)
	assert.Equal(t, int64(450), *result.Models[0].AvgTtftMs)
}

func TestBuildStatusResultCreatesFixedHourlyTimeline(t *testing.T) {
	now := time.Date(2026, 8, 31, 4, 17, 38, 0, time.UTC)
	merged := map[bucketKey]counters{
		{model: "gpt-test", group: "default", bucketTs: now.Add(-70 * time.Minute).Unix()}: {
			requestCount:   10,
			successCount:   8,
			totalLatencyMs: 12000,
			ttftSumMs:      3600,
			ttftCount:      2,
			outputTokens:   450,
			generationMs:   10000,
		},
		{model: "gpt-test", group: "auto", bucketTs: now.Add(-61 * time.Minute).Unix()}: {
			requestCount:   2,
			successCount:   2,
			totalLatencyMs: 2000,
			ttftSumMs:      400,
			ttftCount:      1,
			outputTokens:   50,
			generationMs:   2000,
		},
	}

	result := buildStatusResult(now, 24, merged)

	require.Len(t, result.Models, 1)
	statusModel := result.Models[0]
	require.Len(t, statusModel.Buckets, 24)
	assert.Equal(t, now.Unix(), result.UpdatedAt)
	assert.Equal(t, 24, result.WindowHours)
	assert.Equal(t, int64(12), result.Summary.RequestCount)
	assert.Equal(t, int64(10), result.Summary.SuccessCount)
	assert.InDelta(t, 83.33, result.Summary.SuccessRate, 0.001)
	assert.Equal(t, 1, result.Summary.AttentionCount)
	assert.Equal(t, 0, result.Summary.CriticalCount)
	assert.InDelta(t, 83.33, statusModel.SuccessRate, 0.001)
	require.NotNil(t, statusModel.AvgTtftMs)
	assert.Equal(t, int64(1333), *statusModel.AvgTtftMs)
	require.NotNil(t, statusModel.AvgTps)
	assert.InDelta(t, 41.67, *statusModel.AvgTps, 0.001)
	assert.False(t, statusModel.Buckets[0].HasData)
	assert.Equal(t, now.UTC().Truncate(time.Hour).Add(-23*time.Hour).Unix(), statusModel.Buckets[0].Ts)
	assert.True(t, statusModel.Buckets[22].HasData)
	assert.Equal(t, int64(12), statusModel.Buckets[22].RequestCount)
	assert.InDelta(t, 83.33, statusModel.Buckets[22].SuccessRate, 0.001)
	assert.Equal(t, now.UTC().Truncate(time.Hour).Unix(), statusModel.Buckets[23].Ts)
}

func TestBuildStatusResultUsesWeightedSuccessRateAndStableOrdering(t *testing.T) {
	now := time.Date(2026, 8, 31, 4, 17, 38, 0, time.UTC)
	merged := map[bucketKey]counters{
		{model: "a-critical", group: "default", bucketTs: now.Unix()}: {
			requestCount: 1,
		},
		{model: "b-healthy", group: "default", bucketTs: now.Unix()}: {
			requestCount: 9,
			successCount: 9,
		},
		{model: "ignored-old", group: "default", bucketTs: now.Add(-25 * time.Hour).Unix()}: {
			requestCount: 100,
			successCount: 100,
		},
	}

	result := buildStatusResult(now, 24, merged)

	require.Len(t, result.Models, 2)
	assert.Equal(t, "b-healthy", result.Models[0].ModelName)
	assert.Equal(t, "a-critical", result.Models[1].ModelName)
	assert.InDelta(t, 90.0, result.Summary.SuccessRate, 0.001)
	assert.Equal(t, 1, result.Summary.AttentionCount)
	assert.Equal(t, 1, result.Summary.CriticalCount)
	assert.Nil(t, result.Models[1].AvgTtftMs)
	assert.Nil(t, result.Models[1].AvgTps)
}

func TestBuildStatusResultAppliesStatusThresholds(t *testing.T) {
	now := time.Date(2026, 8, 31, 4, 17, 38, 0, time.UTC)
	merged := map[bucketKey]counters{
		{model: "critical", group: "default", bucketTs: now.Unix()}: {
			requestCount: 10,
			successCount: 6,
		},
		{model: "healthy", group: "default", bucketTs: now.Unix()}: {
			requestCount: 10,
			successCount: 9,
		},
		{model: "warning", group: "default", bucketTs: now.Unix()}: {
			requestCount: 10,
			successCount: 7,
		},
	}

	result := buildStatusResult(now, 24, merged)

	assert.Equal(t, 2, result.Summary.AttentionCount)
	assert.Equal(t, 1, result.Summary.CriticalCount)
}

func TestBuildStatusResultReturnsEmptySnapshotWithoutRequests(t *testing.T) {
	now := time.Date(2026, 8, 31, 4, 17, 38, 0, time.UTC)

	result := buildStatusResult(now, 24, nil)

	assert.Empty(t, result.Models)
	assert.Equal(t, StatusSummary{}, result.Summary)
}

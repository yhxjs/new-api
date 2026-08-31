package model

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func testPerfMetricSummaryBucketsOnDatabase(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	require.False(t, db.Migrator().HasTable(&PerfMetric{}), "database must be dedicated and empty")
	require.NoError(t, db.AutoMigrate(&PerfMetric{}))
	t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&PerfMetric{})) })

	previousDB := DB
	previousType := common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(databaseType)
	initCol()
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousType)
		initCol()
	})

	require.NoError(t, db.Create([]PerfMetric{
		{
			ModelName:      "gpt-test",
			Group:          "default",
			BucketTs:       1000,
			RequestCount:   2,
			SuccessCount:   1,
			TotalLatencyMs: 2000,
			TtftSumMs:      600,
			TtftCount:      1,
			OutputTokens:   20,
			GenerationMs:   1000,
		},
		{
			ModelName:      "gpt-test",
			Group:          "premium",
			BucketTs:       1000,
			RequestCount:   100,
			SuccessCount:   100,
			TotalLatencyMs: 100000,
			TtftSumMs:      10000,
			TtftCount:      100,
			OutputTokens:   1000,
			GenerationMs:   1000,
		},
	}).Error)

	rows, err := GetPerfMetricsSummaryBucketsAll(900, 1100, []string{"default"})

	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(2), rows[0].RequestCount)
	assert.Equal(t, int64(1), rows[0].SuccessCount)
	assert.Equal(t, int64(600), rows[0].TtftSumMs)
	assert.Equal(t, int64(1), rows[0].TtftCount)
}

func TestPerfMetricSummaryBucketsSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	testPerfMetricSummaryBucketsOnDatabase(t, db, common.DatabaseTypeSQLite)
}

func TestPerfMetricSummaryBucketsConfiguredDatabases(t *testing.T) {
	tests := []struct {
		name         string
		env          string
		databaseType common.DatabaseType
		dialector    func(string) gorm.Dialector
	}{
		{
			name:         "mysql",
			env:          "TEST_MYSQL_DSN",
			databaseType: common.DatabaseTypeMySQL,
			dialector: func(dsn string) gorm.Dialector {
				return mysql.Open(dsn)
			},
		},
		{
			name:         "postgres",
			env:          "TEST_POSTGRES_DSN",
			databaseType: common.DatabaseTypePostgreSQL,
			dialector: func(dsn string) gorm.Dialector {
				return postgres.New(postgres.Config{
					DSN:                  dsn,
					PreferSimpleProtocol: true,
				})
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			dsn := strings.TrimSpace(os.Getenv(testCase.env))
			if dsn == "" {
				t.Skip(testCase.env + " is not configured")
			}

			db, err := gorm.Open(testCase.dialector(dsn), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			testPerfMetricSummaryBucketsOnDatabase(t, db, testCase.databaseType)
		})
	}
}

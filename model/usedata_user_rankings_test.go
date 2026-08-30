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

func seedUserRankingQuotaData(t *testing.T, rows ...QuotaData) {
	t.Helper()
	for idx := range rows {
		require.NoError(t, DB.Create(&rows[idx]).Error)
	}
}

func TestGetUserRankingTotalsAggregatesPositiveTokensByUser(t *testing.T) {
	truncateTables(t)
	seedUserRankingQuotaData(t,
		QuotaData{UserID: 1, Username: "alice-old", ModelName: "model-a", CreatedAt: 1100, TokenUsed: 300},
		QuotaData{UserID: 1, Username: "alice-new", ModelName: "model-b", CreatedAt: 1200, TokenUsed: 200},
		QuotaData{UserID: 1, Username: "alice-new", ModelName: "model-c", CreatedAt: 1300, TokenUsed: 500},
		QuotaData{UserID: 2, Username: "bob", ModelName: "model-a", CreatedAt: 1200, TokenUsed: 500},
		QuotaData{UserID: 3, Username: "deleted", ModelName: "model-a", CreatedAt: 1200, TokenUsed: 500},
		QuotaData{UserID: 1, Username: "alice-new", ModelName: "ignored-negative", CreatedAt: 1400, TokenUsed: -100},
		QuotaData{UserID: 1, Username: "alice-new", ModelName: "ignored-zero", CreatedAt: 1400, TokenUsed: 0},
		QuotaData{UserID: 1, Username: "alice-new", ModelName: "outside", CreatedAt: 2100, TokenUsed: 999},
		QuotaData{UserID: 0, Username: "system", ModelName: "model-a", CreatedAt: 1200, TokenUsed: 999},
	)

	rows, err := GetUserRankingTotals(1000, 2000, 20)

	require.NoError(t, err)
	assert.Equal(t, []UserRankingTotal{
		{UserID: 1, TotalTokens: 1000},
		{UserID: 2, TotalTokens: 500},
		{UserID: 3, TotalTokens: 500},
	}, rows)
}

func TestGetUserRankingDetailsReturnModelsAndNameCandidates(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       1,
		Username: "alice-current",
		Password: "unused-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "ranking-aff-1",
	}).Error)
	require.NoError(t, DB.Create(&User{
		Id:       2,
		Username: "bob",
		Password: "unused-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "ranking-aff-2",
	}).Error)
	seedUserRankingQuotaData(t,
		QuotaData{UserID: 1, Username: "alice-old", ModelName: "model-a", CreatedAt: 1100, TokenUsed: 300},
		QuotaData{UserID: 1, Username: "alice-current", ModelName: "model-b", CreatedAt: 1200, TokenUsed: 250},
		QuotaData{UserID: 1, Username: "alice-current", ModelName: "model-c", CreatedAt: 1300, TokenUsed: 150},
		QuotaData{UserID: 1, Username: "alice-current", ModelName: "model-d", CreatedAt: 1400, TokenUsed: 100},
		QuotaData{UserID: 1, Username: "alice-current", ModelName: "model-e", CreatedAt: 1500, TokenUsed: 80},
		QuotaData{UserID: 1, Username: "alice-current", ModelName: "model-f", CreatedAt: 1600, TokenUsed: 70},
		QuotaData{UserID: 1, Username: "alice-current", ModelName: "", CreatedAt: 1700, TokenUsed: 50},
		QuotaData{UserID: 2, Username: "bob", ModelName: "model-a", CreatedAt: 1200, TokenUsed: 400},
		QuotaData{UserID: 3, Username: "deleted-old", ModelName: "model-a", CreatedAt: 1250, TokenUsed: 200},
		QuotaData{UserID: 3, Username: "deleted-latest", ModelName: "model-b", CreatedAt: 1750, TokenUsed: 300},
	)

	models, err := GetUserRankingModelTotals(1000, 2000, []int{1, 3})
	require.NoError(t, err)
	assert.Equal(t, []UserRankingModelTotal{
		{UserID: 1, ModelName: "model-a", TotalTokens: 300},
		{UserID: 1, ModelName: "model-b", TotalTokens: 250},
		{UserID: 1, ModelName: "model-c", TotalTokens: 150},
		{UserID: 1, ModelName: "model-d", TotalTokens: 100},
		{UserID: 1, ModelName: "model-e", TotalTokens: 80},
		{UserID: 1, ModelName: "model-f", TotalTokens: 70},
		{UserID: 1, ModelName: "", TotalTokens: 50},
		{UserID: 3, ModelName: "model-b", TotalTokens: 300},
		{UserID: 3, ModelName: "model-a", TotalTokens: 200},
	}, models)

	currentNames, err := GetUserRankingCurrentNames([]int{1, 2, 3})
	require.NoError(t, err)
	assert.Equal(t, []UserRankingName{
		{UserID: 1, Username: "alice-current"},
		{UserID: 2, Username: "bob"},
	}, currentNames)

	historicalNames, err := GetUserRankingHistoricalNames(1000, 2000, []int{1, 3})
	require.NoError(t, err)
	assert.Equal(t, []UserRankingName{
		{UserID: 1, Username: "alice-current", CreatedAt: 1700},
		{UserID: 1, Username: "alice-old", CreatedAt: 1100},
		{UserID: 3, Username: "deleted-latest", CreatedAt: 1750},
		{UserID: 3, Username: "deleted-old", CreatedAt: 1250},
	}, historicalNames)
}

func testUserRankingQueriesOnDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.False(t, db.Migrator().HasTable(&User{}), "database must be dedicated and empty")
	require.False(t, db.Migrator().HasTable(&QuotaData{}), "database must be dedicated and empty")
	require.NoError(t, db.AutoMigrate(&User{}, &QuotaData{}))
	t.Cleanup(func() {
		require.NoError(t, db.Migrator().DropTable(&QuotaData{}, &User{}))
	})

	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	require.NoError(t, db.Create(&User{
		Id:       1,
		Username: "alice-current",
		Password: "unused-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "ranking-matrix-aff-1",
	}).Error)
	require.NoError(t, db.Create(&User{
		Id:       2,
		Username: "bob",
		Password: "unused-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "ranking-matrix-aff-2",
	}).Error)

	rows := []QuotaData{
		{UserID: 1, Username: "alice-old", ModelName: "model-a", CreatedAt: 1100, TokenUsed: 300},
		{UserID: 1, Username: "alice-current", ModelName: "model-b", CreatedAt: 1200, TokenUsed: 200},
		{UserID: 1, Username: "alice-current", ModelName: "", CreatedAt: 1300, TokenUsed: 100},
		{UserID: 2, Username: "bob", ModelName: "model-a", CreatedAt: 1200, TokenUsed: 500},
		{UserID: 3, Username: "deleted-old", ModelName: "model-a", CreatedAt: 1250, TokenUsed: 200},
		{UserID: 3, Username: "deleted-latest", ModelName: "model-b", CreatedAt: 1750, TokenUsed: 300},
		{UserID: 1, Username: "alice-current", ModelName: "ignored", CreatedAt: 1400, TokenUsed: -50},
		{UserID: 1, Username: "alice-current", ModelName: "outside", CreatedAt: 2100, TokenUsed: 999},
	}
	for idx := range rows {
		require.NoError(t, db.Create(&rows[idx]).Error)
	}

	totals, err := GetUserRankingTotals(1000, 2000, 20)
	require.NoError(t, err)
	assert.Equal(t, []UserRankingTotal{
		{UserID: 1, TotalTokens: 600},
		{UserID: 2, TotalTokens: 500},
		{UserID: 3, TotalTokens: 500},
	}, totals)

	models, err := GetUserRankingModelTotals(1000, 2000, []int{1, 3})
	require.NoError(t, err)
	assert.Equal(t, []UserRankingModelTotal{
		{UserID: 1, ModelName: "model-a", TotalTokens: 300},
		{UserID: 1, ModelName: "model-b", TotalTokens: 200},
		{UserID: 1, ModelName: "", TotalTokens: 100},
		{UserID: 3, ModelName: "model-b", TotalTokens: 300},
		{UserID: 3, ModelName: "model-a", TotalTokens: 200},
	}, models)

	currentNames, err := GetUserRankingCurrentNames([]int{1, 2, 3})
	require.NoError(t, err)
	assert.Equal(t, []UserRankingName{
		{UserID: 1, Username: "alice-current"},
		{UserID: 2, Username: "bob"},
	}, currentNames)

	historicalNames, err := GetUserRankingHistoricalNames(1000, 2000, []int{1, 3})
	require.NoError(t, err)
	assert.Equal(t, []UserRankingName{
		{UserID: 1, Username: "alice-current", CreatedAt: 1400},
		{UserID: 1, Username: "alice-old", CreatedAt: 1100},
		{UserID: 3, Username: "deleted-latest", CreatedAt: 1750},
		{UserID: 3, Username: "deleted-old", CreatedAt: 1250},
	}, historicalNames)
}

func TestUserRankingQueriesSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	testUserRankingQueriesOnDatabase(t, db)
}

func TestUserRankingQueriesConfiguredDatabases(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		dialector func(string) gorm.Dialector
	}{
		{
			name: "mysql",
			env:  "TEST_MYSQL_DSN",
			dialector: func(dsn string) gorm.Dialector {
				return mysql.Open(dsn)
			},
		},
		{
			name: "postgres",
			env:  "TEST_POSTGRES_DSN",
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
			testUserRankingQueriesOnDatabase(t, db)
		})
	}
}

package model

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestUpdateMultiKeyChannelWithSingleConnection(t *testing.T) {
	for _, databaseType := range []common.DatabaseType{
		common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL,
	} {
		t.Run(string(databaseType), func(t *testing.T) {
			var dialector gorm.Dialector
			switch databaseType {
			case common.DatabaseTypeMySQL:
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				dialector = mysql.Open(dsn)
			case common.DatabaseTypePostgreSQL:
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				dialector = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
			default:
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "channels.db") + "?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate")
			}
			db, err := gorm.Open(dialector, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			versionQuery := "SELECT version()"
			if databaseType == common.DatabaseTypeSQLite {
				versionQuery = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database version: %s", version)
			require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))

			previousDB, previousType := DB, common.MainDatabaseType()
			common.SetMainDatabaseType(databaseType)
			t.Cleanup(func() {
				DB = previousDB
				common.SetMainDatabaseType(previousType)
			})
			channel := Channel{
				Type: constant.ChannelTypeOpenAI, Name: "before",
				Key: "sk-first\nsk-second", Models: "gpt-test", Group: "default",
				Status: common.ChannelStatusEnabled,
				ChannelInfo: ChannelInfo{
					IsMultiKey: true, MultiKeySize: 2,
					MultiKeyStatusList: map[int]int{1: common.ChannelStatusManuallyDisabled},
				},
			}
			require.NoError(t, db.Create(&channel).Error)
			t.Cleanup(func() {
				require.NoError(t, db.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error)
				require.NoError(t, db.Delete(&Channel{}, channel.Id).Error)
			})
			// Bound database waits so a connection-pool deadlock fails the test.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			DB = db.WithContext(ctx)
			edited := Channel{Id: channel.Id, Name: "after", ChannelInfo: channel.ChannelInfo}
			require.NoError(t, edited.UpdatePreservingBalanceQueryToken("", false))

			var stored Channel
			require.NoError(t, db.First(&stored, channel.Id).Error)
			assert.Equal(t, "after", stored.Name)
			assert.Equal(t, channel.Key, stored.Key)
			assert.Equal(t, 2, stored.ChannelInfo.MultiKeySize)
			assert.Equal(t, map[int]int{1: common.ChannelStatusManuallyDisabled}, stored.ChannelInfo.MultiKeyStatusList)
			var abilities []Ability
			require.NoError(t, db.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
			require.Len(t, abilities, 1)
			assert.Equal(t, "gpt-test", abilities[0].Model)
			assert.True(t, abilities[0].Enabled)
		})
	}
}

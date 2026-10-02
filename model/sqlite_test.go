package model

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openConfiguredSQLiteForTest(t *testing.T, envName, dsn, query string) *gorm.DB {
	t.Helper()
	t.Setenv(envName, dsn)
	previousPath := common.SQLitePath
	common.SQLitePath = filepath.ToSlash(filepath.Join(t.TempDir(), "custom database.db")) + query
	t.Cleanup(func() { common.SQLitePath = previousPath })
	db, databaseType, err := chooseDB(envName, envName == "LOG_SQL_DSN")
	require.NoError(t, err)
	require.Equal(t, common.DatabaseTypeSQLite, databaseType)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return db
}

func TestChooseDBSQLiteConnectionSettings(t *testing.T) {
	for _, test := range []struct {
		name        string
		envName     string
		dsn         string
		query       string
		busyTimeout int
		journalMode string
		foreignKeys int
	}{
		{"custom path", "SQL_DSN", "", "", 30000, "wal", 0},
		{"local with existing pragma", "SQL_DSN", "local", "?_pragma=foreign_keys(1)", 30000, "wal", 1},
		{"separate log database", "LOG_SQL_DSN", "local", "", 30000, "wal", 0},
		{"explicit parentheses", "SQL_DSN", "", "?_pragma=busy_timeout(17)&_pragma=journal_mode(DELETE)", 17, "delete", 0},
		{"explicit equals and whitespace", "SQL_DSN", "", "?_pragma=+BUSY_TIMEOUT+%3D+19&_pragma=+JOURNAL_MODE+%3D+DELETE&_pragma=foreign_keys(1)", 19, "delete", 1},
		{"explicit quoted schema", "SQL_DSN", "", "?_pragma=%22main%22.%22busy_timeout%22(23)&_pragma=%22main%22.%22journal_mode%22(DELETE)", 23, "delete", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := openConfiguredSQLiteForTest(t, test.envName, test.dsn, test.query)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			// Check two physical connections: PRAGMAs must apply to the entire pool.
			for range 2 {
				conn, err := sqlDB.Conn(context.Background())
				require.NoError(t, err)
				defer conn.Close()
				var busyTimeout, foreignKeys int
				var journalMode string
				require.NoError(t, conn.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&busyTimeout))
				require.NoError(t, conn.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journalMode))
				require.NoError(t, conn.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&foreignKeys))
				assert.Equal(t, test.busyTimeout, busyTimeout)
				assert.Equal(t, test.journalMode, journalMode)
				assert.Equal(t, test.foreignKeys, foreignKeys)
			}
		})
	}
}

func TestChooseDBSQLiteTransactionLock(t *testing.T) {
	for _, test := range []struct {
		name          string
		dsn           string
		query         string
		writerBlocked bool
	}{
		{"default immediate", "", "", true},
		{"local immediate", "local", "?_pragma=foreign_keys(1)", true},
		{"explicit deferred", "", "?_txlock=deferred", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := openConfiguredSQLiteForTest(t, "SQL_DSN", test.dsn, test.query)
			require.NoError(t, db.AutoMigrate(&Channel{}))
			channel := Channel{Name: "before"}
			require.NoError(t, db.Create(&channel).Error)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			first, err := sqlDB.Conn(ctx)
			require.NoError(t, err)
			defer first.Close()
			second, err := sqlDB.Conn(ctx)
			require.NoError(t, err)
			defer second.Close()
			// Fail lock contention immediately, without sleeps or timing assertions.
			_, err = second.ExecContext(ctx, "PRAGMA busy_timeout=0")
			require.NoError(t, err)
			tx, err := first.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback()
			var name string
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT name FROM channels WHERE id = ?", channel.Id).Scan(&name))
			assert.Equal(t, "before", name)
			// WAL keeps autocommit readers available while a writer holds its lock.
			require.NoError(t, second.QueryRowContext(ctx, "SELECT name FROM channels WHERE id = ?", channel.Id).Scan(&name))
			competing, beginErr := second.BeginTx(ctx, nil)
			if competing != nil {
				require.NoError(t, competing.Rollback())
			}
			if test.writerBlocked {
				require.ErrorContains(t, beginErr, "locked")
			} else {
				require.NoError(t, beginErr)
			}
			_, err = tx.ExecContext(ctx, "UPDATE channels SET name = ? WHERE id = ?", "first", channel.Id)
			require.NoError(t, err)
			require.NoError(t, tx.Commit())
			// After the writer commits, a fresh transaction can read and save.
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				var stored Channel
				if err := tx.First(&stored, channel.Id).Error; err != nil {
					return err
				}
				assert.Equal(t, "first", stored.Name)
				return tx.Model(&stored).Update("name", "second").Error
			}))
			var stored Channel
			require.NoError(t, db.First(&stored, channel.Id).Error)
			assert.Equal(t, "second", stored.Name)
			reopened, databaseType, err := chooseDB("SQL_DSN", false)
			require.NoError(t, err)
			require.Equal(t, common.DatabaseTypeSQLite, databaseType)
			reopenedSQL, err := reopened.DB()
			require.NoError(t, err)
			defer reopenedSQL.Close()
			require.NoError(t, reopened.AutoMigrate(&Channel{}))
			require.NoError(t, reopened.First(&stored, channel.Id).Error)
			assert.Equal(t, "second", stored.Name)
		})
	}
}

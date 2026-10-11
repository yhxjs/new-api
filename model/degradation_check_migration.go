package model

// migrateDegradationCheckLogs preserves full model identifiers. Drop the old
// model-name index before widening the column: MySQL cannot index TEXT without
// a prefix, and PostgreSQL's index entry limit also excludes arbitrary names.
// Channel/time indexes still bound the overview and channel log queries.
func migrateDegradationCheckLogs() error {
	migrator := DB.Migrator()
	if migrator.HasIndex(&DegradationCheckLog{}, "idx_degradation_channel_model") {
		if err := migrator.DropIndex(&DegradationCheckLog{}, "idx_degradation_channel_model"); err != nil {
			return err
		}
	}
	return DB.AutoMigrate(&DegradationCheckLog{})
}

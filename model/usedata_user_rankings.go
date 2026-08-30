package model

type UserRankingTotal struct {
	UserID      int   `gorm:"column:user_id"`
	TotalTokens int64 `gorm:"column:total_tokens"`
}

type UserRankingModelTotal struct {
	UserID      int    `gorm:"column:user_id"`
	ModelName   string `gorm:"column:model_name"`
	TotalTokens int64  `gorm:"column:total_tokens"`
}

type UserRankingName struct {
	UserID    int    `gorm:"column:user_id"`
	Username  string `gorm:"column:username"`
	CreatedAt int64  `gorm:"column:created_at"`
}

func GetUserRankingTotals(startTime int64, endTime int64, limit int) ([]UserRankingTotal, error) {
	var rows []UserRankingTotal
	query := DB.Table("quota_data").
		Select("user_id, sum(token_used) AS total_tokens").
		Where("user_id > 0").
		Where("token_used > 0").
		Group("user_id").
		Having("sum(token_used) > 0").
		Order("total_tokens DESC, user_id ASC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&rows).Error
	return rows, err
}

func GetUserRankingModelTotals(startTime int64, endTime int64, userIDs []int) ([]UserRankingModelTotal, error) {
	rows := make([]UserRankingModelTotal, 0)
	if len(userIDs) == 0 {
		return rows, nil
	}

	query := DB.Table("quota_data").
		Select("user_id, model_name, sum(token_used) AS total_tokens").
		Where("user_id IN ?", userIDs).
		Where("token_used > 0").
		Group("user_id, model_name").
		Having("sum(token_used) > 0").
		Order("user_id ASC, total_tokens DESC, model_name ASC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&rows).Error
	return rows, err
}

func GetUserRankingCurrentNames(userIDs []int) ([]UserRankingName, error) {
	rows := make([]UserRankingName, 0)
	if len(userIDs) == 0 {
		return rows, nil
	}

	err := DB.Unscoped().Model(&User{}).
		Select("id AS user_id, username").
		Where("id IN ?", userIDs).
		Order("id ASC").
		Find(&rows).Error
	return rows, err
}

func GetUserRankingHistoricalNames(startTime int64, endTime int64, userIDs []int) ([]UserRankingName, error) {
	rows := make([]UserRankingName, 0)
	if len(userIDs) == 0 {
		return rows, nil
	}

	query := DB.Table("quota_data").
		Select("user_id, username, max(created_at) AS created_at").
		Where("user_id IN ?", userIDs).
		Where("username <> ''").
		Group("user_id, username").
		Order("user_id ASC, created_at DESC, username ASC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&rows).Error
	return rows, err
}

package service

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
)

const (
	userRankingCacheTTL   = 5 * time.Minute
	userRankingLimit      = 20
	userRankingModelLimit = 5
)

type UserRankingsResponse struct {
	Users []UserRanking `json:"users"`
}

type UserRanking struct {
	Rank        int                `json:"rank"`
	Username    string             `json:"username"`
	TotalTokens int64              `json:"total_tokens"`
	Models      []UserRankingModel `json:"models"`
}

type UserRankingModel struct {
	ModelName   string  `json:"model_name"`
	TotalTokens int64   `json:"total_tokens"`
	Share       float64 `json:"share"`
	IsOther     bool    `json:"is_other"`
}

type userRankingCacheItem struct {
	expiresAt time.Time
	data      *UserRankingsResponse
}

var (
	userRankingCacheMu sync.Mutex
	userRankingCache   = map[string]userRankingCacheItem{}
)

func GetUserRankingsSnapshot(period string, revealUsernames bool) (*UserRankingsResponse, error) {
	config, err := rankingConfig(period)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	userRankingCacheMu.Lock()
	if item, ok := userRankingCache[config.id]; ok && now.Before(item.expiresAt) {
		userRankingCacheMu.Unlock()
		return userRankingsView(item.data, revealUsernames), nil
	}
	userRankingCacheMu.Unlock()

	data, err := buildUserRankingsSnapshot(config, now)
	if err != nil {
		return nil, err
	}

	userRankingCacheMu.Lock()
	userRankingCache[config.id] = userRankingCacheItem{
		expiresAt: now.Add(userRankingCacheTTL),
		data:      data,
	}
	userRankingCacheMu.Unlock()

	return userRankingsView(data, revealUsernames), nil
}

func buildUserRankingsSnapshot(config rankingPeriodConfig, now time.Time) (*UserRankingsResponse, error) {
	startTime, endTime := rankingTimeRange(config, now)
	totals, err := model.GetUserRankingTotals(startTime, endTime, userRankingLimit)
	if err != nil {
		return nil, err
	}
	if len(totals) == 0 {
		return &UserRankingsResponse{Users: make([]UserRanking, 0)}, nil
	}

	userIDs := make([]int, 0, len(totals))
	for _, total := range totals {
		userIDs = append(userIDs, total.UserID)
	}

	modelTotals, err := model.GetUserRankingModelTotals(startTime, endTime, userIDs)
	if err != nil {
		return nil, err
	}
	currentNames, err := model.GetUserRankingCurrentNames(userIDs)
	if err != nil {
		return nil, err
	}
	historicalNames, err := model.GetUserRankingHistoricalNames(startTime, endTime, userIDs)
	if err != nil {
		return nil, err
	}

	return buildUserRankings(totals, modelTotals, currentNames, historicalNames), nil
}

func buildUserRankings(totals []model.UserRankingTotal, modelTotals []model.UserRankingModelTotal, currentNames []model.UserRankingName, historicalNames []model.UserRankingName) *UserRankingsResponse {
	orderedTotals := append([]model.UserRankingTotal(nil), totals...)
	sort.Slice(orderedTotals, func(i, j int) bool {
		if orderedTotals[i].TotalTokens == orderedTotals[j].TotalTokens {
			return orderedTotals[i].UserID < orderedTotals[j].UserID
		}
		return orderedTotals[i].TotalTokens > orderedTotals[j].TotalTokens
	})
	if len(orderedTotals) > userRankingLimit {
		orderedTotals = orderedTotals[:userRankingLimit]
	}

	usernameByUser := make(map[int]string, len(currentNames)+len(historicalNames))
	latestNameAt := make(map[int]int64, len(historicalNames))
	for _, item := range historicalNames {
		if item.CreatedAt > latestNameAt[item.UserID] || usernameByUser[item.UserID] == "" {
			usernameByUser[item.UserID] = item.Username
			latestNameAt[item.UserID] = item.CreatedAt
		}
	}
	for _, item := range currentNames {
		if item.Username != "" {
			usernameByUser[item.UserID] = item.Username
		}
	}

	modelsByUser := make(map[int][]model.UserRankingModelTotal)
	for _, item := range modelTotals {
		if item.TotalTokens <= 0 {
			continue
		}
		modelsByUser[item.UserID] = append(modelsByUser[item.UserID], item)
	}

	users := make([]UserRanking, 0, len(orderedTotals))
	for idx, total := range orderedTotals {
		namedModels := make([]model.UserRankingModelTotal, 0, len(modelsByUser[total.UserID]))
		otherTokens := int64(0)
		for _, item := range modelsByUser[total.UserID] {
			if strings.TrimSpace(item.ModelName) == "" {
				otherTokens += item.TotalTokens
				continue
			}
			namedModels = append(namedModels, item)
		}
		sort.Slice(namedModels, func(i, j int) bool {
			if namedModels[i].TotalTokens == namedModels[j].TotalTokens {
				return namedModels[i].ModelName < namedModels[j].ModelName
			}
			return namedModels[i].TotalTokens > namedModels[j].TotalTokens
		})

		models := make([]UserRankingModel, 0, minInt(len(namedModels), userRankingModelLimit)+1)
		for modelIdx, item := range namedModels {
			if modelIdx >= userRankingModelLimit {
				otherTokens += item.TotalTokens
				continue
			}
			models = append(models, UserRankingModel{
				ModelName:   item.ModelName,
				TotalTokens: item.TotalTokens,
				Share:       rankingShare(item.TotalTokens, total.TotalTokens),
			})
		}
		if otherTokens > 0 {
			models = append(models, UserRankingModel{
				TotalTokens: otherTokens,
				Share:       rankingShare(otherTokens, total.TotalTokens),
				IsOther:     true,
			})
		}

		users = append(users, UserRanking{
			Rank:        idx + 1,
			Username:    usernameByUser[total.UserID],
			TotalTokens: total.TotalTokens,
			Models:      models,
		})
	}

	return &UserRankingsResponse{Users: users}
}

func userRankingsView(source *UserRankingsResponse, revealUsernames bool) *UserRankingsResponse {
	if source == nil {
		return nil
	}

	users := make([]UserRanking, len(source.Users))
	for idx, sourceUser := range source.Users {
		users[idx] = sourceUser
		users[idx].Models = append([]UserRankingModel(nil), sourceUser.Models...)
		if !revealUsernames {
			users[idx].Username = maskRankingUsername(sourceUser.Username)
		}
	}
	return &UserRankingsResponse{Users: users}
}

func maskRankingUsername(username string) string {
	runes := []rune(username)
	switch len(runes) {
	case 0:
		return "***"
	case 1:
		return "*"
	default:
		return string(runes[:2]) + "***"
	}
}

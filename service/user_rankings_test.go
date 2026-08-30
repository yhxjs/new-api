package service

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildUserRankingsKeepsTopFiveModelsAndAggregatesOther(t *testing.T) {
	totals := []model.UserRankingTotal{{UserID: 1, TotalTokens: 1000}}
	modelTotals := []model.UserRankingModelTotal{
		{UserID: 1, ModelName: "model-a", TotalTokens: 300},
		{UserID: 1, ModelName: "model-b", TotalTokens: 200},
		{UserID: 1, ModelName: "model-c", TotalTokens: 150},
		{UserID: 1, ModelName: "model-d", TotalTokens: 100},
		{UserID: 1, ModelName: "model-e", TotalTokens: 80},
		{UserID: 1, ModelName: "model-f", TotalTokens: 70},
		{UserID: 1, ModelName: "", TotalTokens: 100},
	}

	result := buildUserRankings(
		totals,
		modelTotals,
		[]model.UserRankingName{{UserID: 1, Username: "alice"}},
		nil,
	)

	require.Len(t, result.Users, 1)
	assert.Equal(t, UserRanking{
		Rank:        1,
		Username:    "alice",
		TotalTokens: 1000,
		Models: []UserRankingModel{
			{ModelName: "model-a", TotalTokens: 300, Share: 0.3},
			{ModelName: "model-b", TotalTokens: 200, Share: 0.2},
			{ModelName: "model-c", TotalTokens: 150, Share: 0.15},
			{ModelName: "model-d", TotalTokens: 100, Share: 0.1},
			{ModelName: "model-e", TotalTokens: 80, Share: 0.08},
			{TotalTokens: 170, Share: 0.17, IsOther: true},
		},
	}, result.Users[0])
}

func TestBuildUserRankingsUsesStableOrderAndResolvesNames(t *testing.T) {
	totals := make([]model.UserRankingTotal, 0, userRankingLimit+1)
	currentNames := make([]model.UserRankingName, 0, userRankingLimit+1)
	for userID := userRankingLimit + 1; userID >= 1; userID-- {
		totals = append(totals, model.UserRankingTotal{UserID: userID, TotalTokens: 100})
		if userID != 1 && userID != 2 {
			currentNames = append(currentNames, model.UserRankingName{
				UserID:   userID,
				Username: fmt.Sprintf("user-%02d", userID),
			})
		}
	}
	currentNames = append(currentNames, model.UserRankingName{UserID: 1, Username: "current-name"})

	result := buildUserRankings(
		totals,
		nil,
		currentNames,
		[]model.UserRankingName{
			{UserID: 1, Username: "ignored-history", CreatedAt: 2000},
			{UserID: 2, Username: "older-name", CreatedAt: 1000},
			{UserID: 2, Username: "latest-name", CreatedAt: 2000},
		},
	)

	require.Len(t, result.Users, userRankingLimit)
	assert.Equal(t, "current-name", result.Users[0].Username)
	assert.Equal(t, "latest-name", result.Users[1].Username)
	for idx, user := range result.Users {
		assert.Equal(t, idx+1, user.Rank)
	}
	assert.Equal(t, "user-20", result.Users[userRankingLimit-1].Username)
}

func TestMaskRankingUsernameUsesUnicodeCharacters(t *testing.T) {
	tests := []struct {
		username string
		want     string
	}{
		{username: "abcdef", want: "ab***"},
		{username: "用户", want: "用户***"},
		{username: "甲", want: "*"},
		{username: "", want: "***"},
	}

	for _, testCase := range tests {
		t.Run(testCase.username, func(t *testing.T) {
			assert.Equal(t, testCase.want, maskRankingUsername(testCase.username))
		})
	}
}

func TestUserRankingsViewDoesNotMutateCachedSnapshot(t *testing.T) {
	source := &UserRankingsResponse{Users: []UserRanking{{
		Rank:        1,
		Username:    "alice",
		TotalTokens: 100,
		Models: []UserRankingModel{{
			ModelName:   "model-a",
			TotalTokens: 100,
			Share:       1,
		}},
	}}}

	anonymous := userRankingsView(source, false)
	authenticated := userRankingsView(source, true)

	assert.Equal(t, "al***", anonymous.Users[0].Username)
	assert.Equal(t, "alice", authenticated.Users[0].Username)
	assert.Equal(t, "alice", source.Users[0].Username)
	anonymous.Users[0].Models[0].ModelName = "changed"
	assert.Equal(t, "model-a", source.Users[0].Models[0].ModelName)
}

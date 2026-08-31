package controller

import (
	"net/http"
	"sort"
	"strings"

	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func GetModelStatus(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Vary", "Authorization, Cookie")

	groupSet := make(map[string]struct{})
	for group := range ratio_setting.GetGroupRatioCopy() {
		groupSet[group] = struct{}{}
	}
	groupSet["auto"] = struct{}{}

	groups := make([]string, 0, len(groupSet))
	for group := range groupSet {
		groups = append(groups, group)
	}
	sort.Strings(groups)

	selectedGroup := strings.TrimSpace(c.DefaultQuery("group", "all"))
	if selectedGroup == "" {
		selectedGroup = "all"
	}
	queryGroups := groups
	if selectedGroup != "all" {
		if _, ok := groupSet[selectedGroup]; !ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "invalid model status group",
			})
			return
		}
		queryGroups = []string{selectedGroup}
	}

	result, err := perfmetrics.QueryStatus(24, queryGroups)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	result.CollectionEnabled = perf_metrics_setting.GetSetting().Enabled
	result.SelectedGroup = selectedGroup
	result.Groups = groups
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

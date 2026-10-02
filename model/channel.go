package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/samber/lo"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Channel struct {
	Id                 int     `json:"id"`
	Type               int     `json:"type" gorm:"default:0"`
	Key                string  `json:"key" gorm:"not null"`
	OpenAIOrganization *string `json:"openai_organization"`
	TestModel          *string `json:"test_model"`
	Status             int     `json:"status" gorm:"default:1"`
	Name               string  `json:"name" gorm:"index"`
	Weight             *uint   `json:"weight" gorm:"default:0"`
	CreatedTime        int64   `json:"created_time" gorm:"bigint"`
	TestTime           int64   `json:"test_time" gorm:"bigint"`
	ResponseTime       int     `json:"response_time"` // in milliseconds
	BaseURL            *string `json:"base_url" gorm:"column:base_url;default:''"`
	Other              string  `json:"other"`
	Balance            float64 `json:"balance"` // in USD
	BalanceUpdatedTime int64   `json:"balance_updated_time" gorm:"bigint"`
	Models             string  `json:"models"`
	Group              string  `json:"group" gorm:"type:varchar(64);default:'default'"`
	UsedQuota          int64   `json:"used_quota" gorm:"bigint;default:0"`
	ModelMapping       *string `json:"model_mapping" gorm:"type:text"`
	//MaxInputTokens     *int    `json:"max_input_tokens" gorm:"default:0"`
	StatusCodeMapping *string `json:"status_code_mapping" gorm:"type:varchar(1024);default:''"`
	Priority          *int64  `json:"priority" gorm:"bigint;default:0"`
	AutoBan           *int    `json:"auto_ban" gorm:"default:1"`
	OtherInfo         string  `json:"other_info"`
	Tag               *string `json:"tag" gorm:"index"`
	Setting           *string `json:"setting" gorm:"type:text"` // 渠道额外设置
	ParamOverride     *string `json:"param_override" gorm:"type:text"`
	HeaderOverride    *string `json:"header_override" gorm:"type:text"`
	Remark            *string `json:"remark" gorm:"type:varchar(255)" validate:"max=255"`
	// add after v0.8.5
	ChannelInfo ChannelInfo `json:"channel_info" gorm:"type:json"`

	OtherSettings string `json:"settings" gorm:"column:settings"` // 其他设置，存储azure版本等不需要检索的信息，详见dto.ChannelOtherSettings

	// cache info
	Keys []string `json:"-" gorm:"-"`
}

type ChannelInfo struct {
	IsMultiKey             bool                  `json:"is_multi_key"`                        // 是否多Key模式
	MultiKeySize           int                   `json:"multi_key_size"`                      // 多Key模式下的Key数量
	MultiKeyStatusList     map[int]int           `json:"multi_key_status_list"`               // key状态列表，key index -> status
	MultiKeyDisabledReason map[int]string        `json:"multi_key_disabled_reason,omitempty"` // key禁用原因列表，key index -> reason
	MultiKeyDisabledTime   map[int]int64         `json:"multi_key_disabled_time,omitempty"`   // key禁用时间列表，key index -> time
	MultiKeyPollingIndex   int                   `json:"multi_key_polling_index"`             // 多Key模式下轮询的key索引
	MultiKeyMode           constant.MultiKeyMode `json:"multi_key_mode"`
	// BalanceQueryLastFailedTime records the most recent failed self-query
	// (user_api/custom) attempt on a New API channel. Balance and
	// BalanceUpdatedTime keep the last SUCCESSFUL value, so this timestamp
	// is what lets the dashboard distinguish a fresh balance from a stale
	// one whose refreshes have been failing.
	BalanceQueryLastFailedTime int64 `json:"balance_query_last_failed_time,omitempty"`
}

type ChannelSortOptions struct {
	SortBy    string
	SortOrder string
	IDSort    bool
}

var channelSortColumns = map[string]string{
	"id":            "id",
	"name":          "name",
	"priority":      "priority",
	"balance":       "balance",
	"response_time": "response_time",
	"test_time":     "test_time",
}

func NewChannelSortOptions(sortBy string, sortOrder string, idSort bool) ChannelSortOptions {
	normalizedSortBy := strings.ToLower(strings.TrimSpace(sortBy))
	normalizedSortOrder := strings.ToLower(strings.TrimSpace(sortOrder))
	if _, ok := channelSortColumns[normalizedSortBy]; !ok {
		normalizedSortBy = ""
		normalizedSortOrder = ""
	} else if normalizedSortOrder != "asc" {
		normalizedSortOrder = "desc"
	}

	return ChannelSortOptions{
		SortBy:    normalizedSortBy,
		SortOrder: normalizedSortOrder,
		IDSort:    idSort,
	}
}

func (options ChannelSortOptions) Apply(query *gorm.DB) *gorm.DB {
	if columnName, ok := channelSortColumns[options.SortBy]; ok {
		return query.Order(clause.OrderByColumn{
			Column: clause.Column{Name: columnName},
			Desc:   options.SortOrder != "asc",
		})
	}
	if options.IDSort {
		return query.Order(clause.OrderByColumn{
			Column: clause.Column{Name: "id"},
			Desc:   true,
		})
	}
	return query.Order(clause.OrderByColumn{
		Column: clause.Column{Name: "priority"},
		Desc:   true,
	})
}

func resolveChannelSortOptions(idSort bool, sortOptions []ChannelSortOptions) ChannelSortOptions {
	if len(sortOptions) == 0 {
		return NewChannelSortOptions("", "", idSort)
	}
	options := sortOptions[0]
	options.IDSort = options.IDSort || idSort
	return options
}

func NormalizeChannelGroupFilter(group string) string {
	group = strings.TrimSpace(group)
	if group == "" || strings.EqualFold(group, "all") || strings.EqualFold(group, "null") {
		return ""
	}
	return group
}

func channelGroupFilterCondition() string {
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return `CONCAT(',', ` + commonGroupCol + `, ',') LIKE ? ESCAPE '!'`
	}
	return `(',' || ` + commonGroupCol + ` || ',') LIKE ? ESCAPE '!'`
}

func channelGroupFilterPattern(group string) string {
	group = strings.NewReplacer(
		"!", "!!",
		"%", "!%",
		"_", "!_",
	).Replace(group)
	return "%," + group + ",%"
}

func ApplyChannelGroupFilter(query *gorm.DB, group string) *gorm.DB {
	group = NormalizeChannelGroupFilter(group)
	if group == "" {
		return query
	}
	return query.Where(channelGroupFilterCondition(), channelGroupFilterPattern(group))
}

// Value implements driver.Valuer interface
// 必须返回 string 而非 []byte:PG simple protocol 下 []byte 参数按 bytea
// 编码,写 json 列会触发 SQLSTATE 22P02。
func (c ChannelInfo) Value() (driver.Value, error) {
	b, err := common.Marshal(&c)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner interface
func (c *ChannelInfo) Scan(value interface{}) error {
	return common.Unmarshal(jsonScanBytes(value), c)
}

func (channel *Channel) GetKeys() []string {
	if channel.Key == "" {
		return []string{}
	}
	if len(channel.Keys) > 0 {
		return channel.Keys
	}
	trimmed := strings.TrimSpace(channel.Key)
	// If the key starts with '[', try to parse it as a JSON array (e.g., for Vertex AI scenarios)
	if strings.HasPrefix(trimmed, "[") {
		var arr []json.RawMessage
		if err := common.Unmarshal([]byte(trimmed), &arr); err == nil {
			res := make([]string, len(arr))
			for i, v := range arr {
				res[i] = string(v)
			}
			return res
		}
	}
	// Otherwise, fall back to splitting by newline
	keys := strings.Split(strings.Trim(channel.Key, "\n"), "\n")
	return keys
}

func (channel *Channel) GetNextEnabledKey() (string, int, *types.NewAPIError) {
	// If not in multi-key mode, return the original key string directly.
	if !channel.ChannelInfo.IsMultiKey {
		return channel.Key, 0, nil
	}

	// Obtain all keys (split by \n)
	keys := channel.GetKeys()
	if len(keys) == 0 {
		// No keys available, return error, should disable the channel
		return "", 0, types.NewError(errors.New("no keys available"), types.ErrorCodeChannelNoAvailableKey)
	}

	lock := GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()

	statusList := channel.ChannelInfo.MultiKeyStatusList
	// helper to get key status, default to enabled when missing
	getStatus := func(idx int) int {
		if statusList == nil {
			return common.ChannelStatusEnabled
		}
		if status, ok := statusList[idx]; ok {
			return status
		}
		return common.ChannelStatusEnabled
	}

	// Collect indexes of enabled keys
	enabledIdx := make([]int, 0, len(keys))
	for i := range keys {
		if getStatus(i) == common.ChannelStatusEnabled {
			enabledIdx = append(enabledIdx, i)
		}
	}
	// If no specific status list or none enabled, return an explicit error so caller can
	// properly handle a channel with no available keys (e.g. mark channel disabled).
	// Returning the first key here caused requests to keep using an already-disabled key.
	if len(enabledIdx) == 0 {
		return "", 0, types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey)
	}

	switch channel.ChannelInfo.MultiKeyMode {
	case constant.MultiKeyModeRandom:
		// Randomly pick one enabled key
		selectedIdx := enabledIdx[rand.Intn(len(enabledIdx))]
		return keys[selectedIdx], selectedIdx, nil
	case constant.MultiKeyModePolling:
		// Use channel-specific lock to ensure thread-safe polling

		channelInfo, err := CacheGetChannelInfo(channel.Id)
		if err != nil {
			return "", 0, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		defer func() {
			if common.DebugEnabled {
				logger.LogDebug(nil, "channel %d polling index: %d", channel.Id, channel.ChannelInfo.MultiKeyPollingIndex)
			}
			if !common.MemoryCacheEnabled {
				_ = channel.SaveChannelInfo()
			} else {
				// CacheUpdateChannel(channel)
			}
		}()
		// Start from the saved polling index and look for the next enabled key
		start := channelInfo.MultiKeyPollingIndex
		if start < 0 || start >= len(keys) {
			start = 0
		}
		for i := 0; i < len(keys); i++ {
			idx := (start + i) % len(keys)
			if getStatus(idx) == common.ChannelStatusEnabled {
				// update polling index for next call (point to the next position)
				channel.ChannelInfo.MultiKeyPollingIndex = (idx + 1) % len(keys)
				return keys[idx], idx, nil
			}
		}
		// Fallback – should not happen, but return first enabled key
		return keys[enabledIdx[0]], enabledIdx[0], nil
	default:
		// Unknown mode, default to first enabled key (or original key string)
		return keys[enabledIdx[0]], enabledIdx[0], nil
	}
}

func (channel *Channel) SaveChannelInfo() error {
	return DB.Model(channel).Update("channel_info", channel.ChannelInfo).Error
}

func (channel *Channel) GetModels() []string {
	if channel.Models == "" {
		return []string{}
	}
	return strings.Split(strings.Trim(channel.Models, ","), ",")
}

func (channel *Channel) GetGroups() []string {
	if channel.Group == "" {
		return []string{}
	}
	groups := strings.Split(strings.Trim(channel.Group, ","), ",")
	for i, group := range groups {
		groups[i] = strings.TrimSpace(group)
	}
	return groups
}

func (channel *Channel) GetOtherInfo() map[string]interface{} {
	otherInfo := make(map[string]interface{})
	if channel.OtherInfo != "" {
		err := common.Unmarshal([]byte(channel.OtherInfo), &otherInfo)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		}
	}
	return otherInfo
}

func (channel *Channel) SetOtherInfo(otherInfo map[string]interface{}) {
	otherInfoBytes, err := json.Marshal(otherInfo)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		return
	}
	channel.OtherInfo = string(otherInfoBytes)
}

func (channel *Channel) GetTag() string {
	if channel.Tag == nil {
		return ""
	}
	return *channel.Tag
}

func (channel *Channel) SetTag(tag string) {
	channel.Tag = &tag
}

func (channel *Channel) GetAutoBan() bool {
	if channel.AutoBan == nil {
		return false
	}
	return *channel.AutoBan == 1
}

func (channel *Channel) Save() error {
	return DB.Save(channel).Error
}

// saveStatusState persists only the fields owned by the channel status flow.
// Keeping this allowlist here prevents a stale channel snapshot from
// overwriting credentials, accounting counters, or channel configuration.
func (channel *Channel) saveStatusState() error {
	if channel.Id == 0 {
		return errors.New("channel ID is 0")
	}
	updates := map[string]any{
		"status":     channel.Status,
		"other_info": channel.OtherInfo,
	}
	if channel.ChannelInfo.IsMultiKey {
		updates["channel_info"] = channel.ChannelInfo
	}
	return DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(updates).Error
}

func GetAllChannels(startIdx int, num int, selectAll bool, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	var err error
	order := resolveChannelSortOptions(idSort, sortOptions)
	if selectAll {
		err = order.Apply(DB).Find(&channels).Error
	} else {
		err = order.Apply(DB).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	}
	return channels, err
}

func GetChannelsByTag(tag string, idSort bool, selectAll bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	order := resolveChannelSortOptions(idSort, sortOptions)
	query := order.Apply(DB.Where("tag = ?", tag))
	if !selectAll {
		query = query.Omit("key")
	}
	err := query.Find(&channels).Error
	return channels, err
}

func SearchChannels(keyword string, group string, model string, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	modelsCol := "`models`"

	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	order := resolveChannelSortOptions(idSort, sortOptions)

	// 构造基础查询
	baseQuery := DB.Model(&Channel{}).Omit("key")

	// 构造WHERE子句
	whereClause := "(id = ? OR name LIKE ? OR " + commonKeyCol + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	baseQuery = ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)

	// 执行查询
	err := order.Apply(baseQuery).Find(&channels).Error
	if err != nil {
		return nil, err
	}
	return channels, nil
}

// GetChannelById loads a channel directly from the database, bypassing the
// in-memory channel cache.
//
// WARNING: do NOT call this on request hot paths (middleware, distribution,
// relay submit/retry, polling). Every call is a synchronous DB query and will
// not see cache-only state. Use CacheGetChannel instead: it serves from the
// in-memory cache and falls back to this function automatically when
// MemoryCacheEnabled is false. Direct use is appropriate only where fresh DB
// state is required, e.g. admin CRUD, channel testing, or cache (re)building.
func GetChannelById(id int, selectAll bool) (*Channel, error) {
	channel := &Channel{Id: id}
	var err error = nil
	if selectAll {
		err = DB.First(channel, "id = ?", id).Error
	} else {
		err = DB.Omit("key").First(channel, "id = ?", id).Error
	}
	if err != nil {
		return nil, err
	}
	return channel, nil
}

func BatchInsertChannels(channels []Channel) error {
	if len(channels) == 0 {
		return nil
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	for _, chunk := range lo.Chunk(channels, 50) {
		if err := tx.Create(&chunk).Error; err != nil {
			tx.Rollback()
			return err
		}
		for _, channel_ := range chunk {
			if err := channel_.AddAbilities(tx); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit().Error
}

func BatchDeleteChannels(ids []int) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	// 使用事务 分批删除channel表和abilities表
	tx := DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}
	var deletedCount int64
	for _, chunk := range lo.Chunk(ids, 200) {
		result := tx.Where("id in (?)", chunk).Delete(&Channel{})
		if result.Error != nil {
			tx.Rollback()
			return 0, result.Error
		}
		deletedCount += result.RowsAffected
		if err := tx.Where("channel_id in (?)", chunk).Delete(&Ability{}).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return deletedCount, nil
}

func (channel *Channel) GetPriority() int64 {
	if channel.Priority == nil {
		return 0
	}
	return *channel.Priority
}

func (channel *Channel) GetWeight() int {
	if channel.Weight == nil {
		return 0
	}
	return int(*channel.Weight)
}

func (channel *Channel) GetBaseURL() string {
	if channel.BaseURL == nil {
		return ""
	}
	url := *channel.BaseURL
	if url == "" {
		url = constant.GetChannelBaseURL(channel.Type)
	}
	return url
}

func (channel *Channel) GetModelMapping() string {
	if channel.ModelMapping == nil {
		return ""
	}
	return *channel.ModelMapping
}

func (channel *Channel) GetStatusCodeMapping() string {
	if channel.StatusCodeMapping == nil {
		return ""
	}
	return *channel.StatusCodeMapping
}

func (channel *Channel) Insert() error {
	var err error
	err = DB.Create(channel).Error
	if err != nil {
		return err
	}
	err = channel.AddAbilities(nil)
	return err
}

func (channel *Channel) Update() error {
	if err := channel.update(DB); err != nil {
		return err
	}
	return channel.UpdateAbilities(nil)
}

// update performs the multi-key size fixup, the row update, and the re-read on
// the given database handle so the write can join a caller's transaction. The
// re-read is intentional: it refreshes the caller's in-memory channel from the
// persisted row (including the freshly merged settings) before the transaction
// commits, so callers reading the channel after UpdatePreservingBalanceQueryToken
// see the committed values rather than the request-time snapshot.
func (channel *Channel) update(db *gorm.DB) error {
	// If this is a multi-key channel, recalculate MultiKeySize based on the current key list to avoid inconsistency after editing keys
	if channel.ChannelInfo.IsMultiKey {
		var keyStr string
		if channel.Key != "" {
			keyStr = channel.Key
		} else {
			// If key is not provided, read the existing key from the database
			var existing Channel
			if err := db.Select("key").First(&existing, "id = ?", channel.Id).Error; err != nil {
				return err
			}
			keyStr = existing.Key
		}
		// Parse the key list (supports newline separation or JSON array)
		keys := []string{}
		if keyStr != "" {
			trimmed := strings.TrimSpace(keyStr)
			if strings.HasPrefix(trimmed, "[") {
				var arr []json.RawMessage
				if err := common.Unmarshal([]byte(trimmed), &arr); err == nil {
					keys = make([]string, len(arr))
					for i, v := range arr {
						keys[i] = string(v)
					}
				}
			}
			if len(keys) == 0 { // fallback to newline split
				keys = strings.Split(strings.Trim(keyStr, "\n"), "\n")
			}
		}
		channel.ChannelInfo.MultiKeySize = len(keys)
		// Clean up status data that exceeds the new key count to prevent index out of range
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			for idx := range channel.ChannelInfo.MultiKeyStatusList {
				if idx >= channel.ChannelInfo.MultiKeySize {
					delete(channel.ChannelInfo.MultiKeyStatusList, idx)
				}
			}
		}
	}
	var err error
	err = db.Model(channel).Updates(channel).Error
	if err != nil {
		return err
	}
	db.Model(channel).First(channel, "id = ?", channel.Id)
	return nil
}

// UpdatePreservingBalanceQueryToken updates the channel like Update, and
// re-applies the "empty token keeps the stored token" merge inside the same
// transaction as the write. The controller validated the request against an
// origin snapshot read without a lock; if a concurrent update rotated the
// balance-query access token in the meantime, merging from that snapshot and
// writing would silently revert the rotation. The merge therefore restarts
// from the raw incoming settings (captured before the controller restored the
// origin token for validation) and re-reads the stored token under
// SELECT ... FOR UPDATE (no-op on SQLite, whose single-writer model
// serializes writers anyway), so the committed row always keeps whichever
// balance-query token is current when this update lands.
//
// Scope note: the row lock serializes only the balance-query token merge.
// Every other field (name, models, base_url, ...) is still written from the
// controller's lock-free origin snapshot, so two concurrent edits to
// non-settings fields remain last-writer-wins — the same behavior as Update.
// Settings are re-validated under the same lock: the merge may have pulled in
// a concurrently rotated token (or none, if a concurrent update cleared it),
// and non-NewAPI rows must not retain a stale balance_query block the
// controller-side validation already stripped from its restored copy.
func (channel *Channel) UpdatePreservingBalanceQueryToken(incomingOtherSettings string, settingsProvided bool) error {
	err := DB.Transaction(func(tx *gorm.DB) error {
		stored := &Channel{}
		if err := lockForUpdate(tx).First(stored, "id = ?", channel.Id).Error; err != nil {
			return err
		}
		settingsChannel := *channel
		if settingsChannel.Type == 0 {
			settingsChannel.Type = stored.Type
		}
		if settingsChannel.BaseURL == nil {
			settingsChannel.BaseURL = stored.BaseURL
		}
		settingsChannel.OtherSettings = stored.OtherSettings
		if settingsProvided {
			settingsChannel.OtherSettings = incomingOtherSettings
		}
		// Balance query settings only exist on New API channels; for every
		// other type the merge must not re-add the credential, and the
		// re-validation below strips any stale block from the raw snapshot.
		if settingsChannel.Type == constant.ChannelTypeNewAPI {
			settingsChannel.RestoreBalanceQueryAccessToken(stored)
		}
		if err := settingsChannel.ValidateSettings(); err != nil {
			return err
		}
		channel.OtherSettings = settingsChannel.OtherSettings
		// Editing must not overwrite a failure recorded or cleared after the
		// controller read its ChannelInfo snapshot.
		channel.ChannelInfo.BalanceQueryLastFailedTime = stored.ChannelInfo.BalanceQueryLastFailedTime
		clearSettings := settingsProvided && settingsChannel.OtherSettings == ""
		if err := channel.update(tx); err != nil {
			return err
		}
		if clearSettings {
			if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).Update("settings", "").Error; err != nil {
				return err
			}
			channel.OtherSettings = ""
		}
		return nil
	})
	if err != nil {
		return err
	}
	return channel.UpdateAbilities(nil)
}

// UpdateUpstreamModelSettings merges only the model-discovery fields into the
// current settings row. Model discovery performs an upstream request before
// persisting its result, so the caller's settings snapshot can be stale by
// the time it writes. Keeping the merge inside a row-locked transaction
// prevents unrelated credentials, including balance-query tokens, from being
// reverted by that stale snapshot.
func (channel *Channel) UpdateUpstreamModelSettings(settings dto.ChannelOtherSettings, updateModels bool) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var stored Channel
		if err := lockForUpdate(tx).Select("id", "settings", "models").First(&stored, "id = ?", channel.Id).Error; err != nil {
			return err
		}

		currentSettings := map[string]json.RawMessage{}
		if strings.TrimSpace(stored.OtherSettings) != "" {
			if err := common.UnmarshalJsonStr(stored.OtherSettings, &currentSettings); err != nil {
				return err
			}
		}
		updatedSettingsBytes, err := common.Marshal(settings)
		if err != nil {
			return err
		}
		updatedSettings := map[string]json.RawMessage{}
		if err := common.Unmarshal(updatedSettingsBytes, &updatedSettings); err != nil {
			return err
		}
		modelUpdateFields := []string{
			"upstream_model_update_check_enabled",
			"upstream_model_update_auto_sync_enabled",
			"upstream_model_update_last_check_time",
			"upstream_model_update_last_detected_models",
			"upstream_model_update_last_removed_models",
			"upstream_model_update_ignored_models",
		}
		// Preserve all unrelated fields in source order, including duplicate
		// objects and case aliases that the settings DTO merges in that order.
		fields := []string{}
		gjson.Parse(stored.OtherSettings).ForEach(func(name, value gjson.Result) bool {
			if name.Type != gjson.String {
				return true // A null settings object has no fields to preserve.
			}
			for _, key := range modelUpdateFields {
				if strings.EqualFold(name.Str, key) {
					return true
				}
			}
			fields = append(fields, name.Raw+":"+value.Raw)
			return true
		})
		for _, key := range modelUpdateFields {
			if value, ok := updatedSettings[key]; ok {
				fields = append(fields, `"`+key+`":`+string(value))
			}
		}
		mergedSettings := "{" + strings.Join(fields, ",") + "}"
		updates := map[string]any{"settings": mergedSettings}
		if updateModels {
			updates["models"] = channel.Models
		}
		if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).Updates(updates).Error; err != nil {
			return err
		}
		channel.OtherSettings = mergedSettings
		return nil
	})
}

func (channel *Channel) UpdateResponseTime(responseTime int64) {
	err := DB.Model(channel).Select("response_time", "test_time").Updates(Channel{
		TestTime:     common.GetTimestamp(),
		ResponseTime: int(responseTime),
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update response time: channel_id=%d, error=%v", channel.Id, err))
	}
}

func (channel *Channel) UpdateBalance(balance float64) {
	err := DB.Model(channel).Select("balance_updated_time", "balance").Updates(Channel{
		BalanceUpdatedTime: common.GetTimestamp(),
		Balance:            balance,
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update balance: channel_id=%d, error=%v", channel.Id, err))
	}
}

// ClearBalanceQueryFailure preserves other channel-info fields by merging
// against the current row rather than a potentially stale query snapshot.
func (channel *Channel) ClearBalanceQueryFailure() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var stored Channel
		if err := lockForUpdate(tx).Select("id", "channel_info").First(&stored, "id = ?", channel.Id).Error; err != nil {
			return err
		}
		if stored.ChannelInfo.BalanceQueryLastFailedTime == 0 {
			return nil
		}
		stored.ChannelInfo.BalanceQueryLastFailedTime = 0
		return tx.Model(&stored).Update("channel_info", stored.ChannelInfo).Error
	})
}

// MarkBalanceQueryFailure updates only the current failure timestamp so an
// automatic refresh cannot overwrite concurrent channel-info changes.
func (channel *Channel) MarkBalanceQueryFailure(timestamp int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var stored Channel
		if err := lockForUpdate(tx).Select("id", "channel_info").First(&stored, "id = ?", channel.Id).Error; err != nil {
			return err
		}
		stored.ChannelInfo.BalanceQueryLastFailedTime = timestamp
		return tx.Model(&stored).Update("channel_info", stored.ChannelInfo).Error
	})
}

// RedactBalanceQueryAccessToken clears the user_api access token and masks
// custom request credentials in the channel's other settings. The edit is
// done on the raw settings JSON
// (not through the settings DTO) so keys unknown to the DTO survive the
// redacted round-trip. Channel responses to ChannelRead admins must be
// redacted; credentials stay readable only through the dedicated secure
// key-view endpoint and inside the relay/balance code paths.
//
// Redaction never fails open: whenever the token cannot be removed
// surgically (unparseable settings, unreadable balance_query object), the
// affected part is dropped from the API copy instead of traveling to the
// client with the token still inside.
func (channel *Channel) RedactBalanceQueryAccessToken() {
	if strings.TrimSpace(channel.OtherSettings) == "" {
		return
	}
	settings := map[string]json.RawMessage{}
	if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
		// Unparseable settings cannot have the token removed surgically, so
		// the API copy carries no settings at all.
		common.SysError(fmt.Sprintf("redact balance query access token: channel %d has unparseable settings JSON", channel.Id))
		channel.OtherSettings = ""
		return
	}
	settingsChanged := false
	fields := []string{}
	// Preserve field order: the settings DTO accepts aliases and applies them
	// in JSON order, so sorting duplicate aliases can change the active token.
	gjson.Parse(channel.OtherSettings).ForEach(func(queryKey, value gjson.Result) bool {
		raw := value.Raw
		if strings.EqualFold(queryKey.Str, "balance_query") && value.Type != gjson.Null {
			if value.Type != gjson.JSON || !value.IsObject() {
				common.SysError(fmt.Sprintf("redact balance query access token: channel %d has unparseable balance_query JSON", channel.Id))
				settingsChanged = true
				return true
			}
			queryFields := []string{}
			value.ForEach(func(tokenKey, access gjson.Result) bool {
				if strings.EqualFold(tokenKey.Str, "access_token") &&
					access.Type != gjson.Null && !(access.Type == gjson.String && access.Str == "") {
					if access.Type != gjson.String {
						common.SysError(fmt.Sprintf("redact balance query access token: channel %d has a non-string access_token", channel.Id))
					}
					settingsChanged = true
					return true
				}
				name, _ := common.Marshal(tokenKey.Str)
				queryFields = append(queryFields, string(name)+":"+access.Raw)
				return true
			})
			raw = "{" + strings.Join(queryFields, ",") + "}"
		}
		name, _ := common.Marshal(queryKey.Str)
		fields = append(fields, string(name)+":"+raw)
		return true
	})
	if settingsChanged {
		channel.OtherSettings = "{" + strings.Join(fields, ",") + "}"
	}
	if settings != nil {
		channel.OtherSettings = balanceQueryRequestCredentials(channel.OtherSettings, nil)
	}
}

// RestoreBalanceQueryAccessToken merges a possibly-redacted incoming balance
// query config (from a channel update request) with the stored one: when the
// new config keeps user_api mode but carries no access token, the stored
// token is preserved, mirroring the empty-key-keeps-existing-key semantics.
// Custom request markers are replaced with the corresponding stored values.
// The merge edits the raw settings JSON in place so keys unknown to the
// settings DTO survive the round-trip, like RedactBalanceQueryAccessToken.
func (channel *Channel) RestoreBalanceQueryAccessToken(origin *Channel) {
	// Parsing an unvalidated request must never invoke GetOtherSettings:
	// that getter repairs malformed settings by saving the entire channel.
	var incomingSettings, savedSettings dto.ChannelOtherSettings
	if channel.OtherSettings != "" {
		if err := common.UnmarshalJsonStr(channel.OtherSettings, &incomingSettings); err != nil {
			return
		}
	}
	incoming := incomingSettings.BalanceQuery
	if incoming == nil {
		return
	}
	if incoming.NormalizedMode() == dto.BalanceQueryModeCustom {
		channel.OtherSettings = balanceQueryRequestCredentials(channel.OtherSettings, &origin.OtherSettings)
		return
	}
	if incoming.NormalizedMode() != dto.BalanceQueryModeUserAPI {
		return
	}
	if strings.TrimSpace(incoming.AccessToken) != "" {
		return
	}
	if origin.OtherSettings != "" {
		if err := common.UnmarshalJsonStr(origin.OtherSettings, &savedSettings); err != nil {
			return
		}
	}
	saved := savedSettings.BalanceQuery
	if saved == nil || strings.TrimSpace(saved.AccessToken) == "" {
		return
	}
	token, err := common.Marshal(saved.AccessToken)
	if err != nil {
		return
	}
	savedTokens := [][]string{}
	savedQueryKeys := []string{}
	gjson.Parse(origin.OtherSettings).ForEach(func(queryKey, value gjson.Result) bool {
		if !strings.EqualFold(queryKey.Str, "balance_query") || !value.IsObject() {
			return true
		}
		queryTokens := []string{}
		value.ForEach(func(name, field gjson.Result) bool {
			if strings.EqualFold(name.Str, "access_token") {
				key, _ := common.Marshal(name.Str)
				queryTokens = append(queryTokens, string(key)+":"+field.Raw)
			}
			return true
		})
		savedTokens = append(savedTokens, queryTokens)
		savedQueryKeys = append(savedQueryKeys, queryKey.Str)
		return true
	})
	incomingQueryKeys := []string{}
	gjson.Parse(channel.OtherSettings).ForEach(func(queryKey, value gjson.Result) bool {
		if strings.EqualFold(queryKey.Str, "balance_query") && value.IsObject() {
			incomingQueryKeys = append(incomingQueryKeys, queryKey.Str)
		}
		return true
	})
	preserveAliases := len(savedQueryKeys) == len(incomingQueryKeys)
	if preserveAliases {
		for index, name := range savedQueryKeys {
			if name != incomingQueryKeys[index] {
				preserveAliases = false
				break
			}
		}
	}
	queryIndex := 0
	fields := []string{}
	gjson.Parse(channel.OtherSettings).ForEach(func(queryKey, value gjson.Result) bool {
		raw := value.Raw
		if strings.EqualFold(queryKey.Str, "balance_query") && value.IsObject() {
			queryFields := []string{}
			value.ForEach(func(name, field gjson.Result) bool {
				if !strings.EqualFold(name.Str, "access_token") {
					key, _ := common.Marshal(name.Str)
					queryFields = append(queryFields, string(key)+":"+field.Raw)
				}
				return true
			})
			if preserveAliases {
				queryFields = append(queryFields, savedTokens[queryIndex]...)
			} else {
				// Use one canonical token field for a new duplicate object. The
				// DTO merges duplicate objects in source order, so every object
				// must remain valid after a redacted round-trip.
				queryFields = append(queryFields, `"access_token":`+string(token))
			}
			queryIndex++
			raw = "{" + strings.Join(queryFields, ",") + "}"
		}
		name, _ := common.Marshal(queryKey.Str)
		fields = append(fields, string(name)+":"+raw)
		return true
	})
	channel.OtherSettings = "{" + strings.Join(fields, ",") + "}"
}

// canonicalizeOtherSettingsForCompare normalizes the channel's other settings
// for the sensitive-changes comparison. It returns the canonical JSON, a
// credential fingerprint (including every token alias in JSON order),
// and whether the settings were parseable at all. The canonical form keeps the
// raw settings JSON (not the settings DTO) so keys unknown to the DTO stay
// visible to the sensitive-changes check: an edit that touches them still
// requires ChannelSensitiveWrite. Values keep byte fidelity — raw JSON, never
// round-tripped through float64 — so two different numbers cannot collapse
// into an equal comparison; only the balance_query object is re-marshaled
// (key order normalized) so the token can be excised. An unreadable
// balance_query object carries no comparable credential and is dropped the
// same way RedactBalanceQueryAccessToken drops it from responses. A
// balance_query reduced to the bare subscription default (nothing beyond the
// mode) counts as absent, so a frontend-injected {"mode":"subscription"}
// equals a legacy channel that never stored balance query settings.
//
// Invariant: the canonical form never moves or drops keys other than
// balance_query, so a non-empty settings map always canonicalizes to a
// non-empty JSON object.
func (channel *Channel) canonicalizeOtherSettingsForCompare() (string, string, bool) {
	trimmed := strings.TrimSpace(channel.OtherSettings)
	if trimmed == "" {
		trimmed = "{}"
	}
	var settings map[string]json.RawMessage
	if err := common.UnmarshalJsonStr(trimmed, &settings); err != nil {
		return "", "", false
	}
	if settings == nil {
		settings = map[string]json.RawMessage{}
	}
	token := ""
	credentials := []string{}
	orderedBalance := []string{}
	orderedOther := []string{}
	seenOtherFields := map[string]bool{}
	hasDuplicateOtherField := false
	hasDuplicateBalanceField := false
	gjson.Parse(trimmed).ForEach(func(queryKey, value gjson.Result) bool {
		if !strings.EqualFold(queryKey.Str, "balance_query") {
			// JSON's ASCII field matching also folds the Unicode long s.
			fieldName := strings.ReplaceAll(strings.ToLower(queryKey.Str), "ſ", "s")
			if seenOtherFields[fieldName] {
				hasDuplicateOtherField = true
			}
			seenOtherFields[fieldName] = true
			key, _ := common.Marshal(queryKey.Str)
			orderedOther = append(orderedOther, string(key)+":"+value.Raw)
			return true
		}
		if value.IsObject() {
			queryFields := []string{}
			seenFields := map[string]bool{}
			value.ForEach(func(tokenKey, access gjson.Result) bool {
				if strings.EqualFold(tokenKey.Str, "access_token") {
					var tokenString string
					if err := common.UnmarshalJsonStr(access.Raw, &tokenString); err != nil {
						tokenString = access.Raw
					}
					token = tokenString
					credentials = append(credentials, queryKey.Str+"."+tokenKey.Str+":"+tokenString)
					return true
				}
				fieldName := strings.ToLower(tokenKey.Str)
				for _, knownField := range []string{"mode", "user_id", "quota_per_unit", "method", "url", "headers", "body", "extract"} {
					if strings.EqualFold(tokenKey.Str, knownField) {
						fieldName = knownField
						break
					}
				}
				if seenFields[fieldName] {
					hasDuplicateBalanceField = true
				}
				seenFields[fieldName] = true
				key, _ := common.Marshal(tokenKey.Str)
				queryFields = append(queryFields, string(key)+":"+access.Raw)
				return true
			})
			orderedBalance = append(orderedBalance, "{"+strings.Join(queryFields, ",")+"}")
		} else {
			orderedBalance = append(orderedBalance, value.Raw)
		}
		return true
	})
	if len(credentials) > 1 {
		hasDuplicateBalanceField = true
		encoded, err := common.Marshal(credentials)
		if err != nil {
			return "", "", false
		}
		token = string(encoded)
	}
	if len(orderedBalance) > 1 {
		hasDuplicateBalanceField = true
	}
	if hasDuplicateOtherField {
		// Top-level aliases outside balance_query are also merged in source
		// order. Sorting them could hide a change to the effective relay route
		// or passthrough settings from the sensitive-write permission check.
		return "ordered-other:{" + strings.Join(orderedOther, ",") + "}:[" + strings.Join(orderedBalance, ",") + "]", token, true
	}
	if hasDuplicateBalanceField {
		// Preserve duplicate objects and case-variant field order in the
		// comparison. encoding/json merges these fields into one DTO value,
		// so a map-based canonical form could authorize a request that changes
		// the effective balance-query request.
		other := map[string]json.RawMessage{}
		for queryKey, raw := range settings {
			if !strings.EqualFold(queryKey, "balance_query") {
				other[queryKey] = raw
			}
		}
		otherJSON, err := common.Marshal(other)
		if err != nil {
			return "", "", false
		}
		return "ordered:" + "[" + strings.Join(orderedBalance, ",") + "]:" + string(otherJSON), token, true
	}
	for queryKey, balanceQueryRaw := range settings {
		if !strings.EqualFold(queryKey, "balance_query") {
			continue
		}
		balanceQuery := map[string]json.RawMessage{}
		if err := common.Unmarshal(balanceQueryRaw, &balanceQuery); err != nil {
			delete(settings, queryKey)
		} else {
			for tokenKey := range balanceQuery {
				if strings.EqualFold(tokenKey, "access_token") {
					delete(balanceQuery, tokenKey)
				}
			}
			reduced := len(balanceQuery) == 0
			if !reduced && len(balanceQuery) == 1 {
				var mode string
				if err := common.Unmarshal(balanceQuery["mode"], &mode); err == nil &&
					(mode == "" || mode == dto.BalanceQueryModeSubscription) {
					reduced = true
				}
			}
			if reduced {
				delete(settings, queryKey)
			} else if canonicalQuery, err := common.Marshal(balanceQuery); err == nil {
				settings[queryKey] = canonicalQuery
			} else {
				return "", "", false
			}
		}
	}
	canonicalJSON, err := common.Marshal(settings)
	if err != nil {
		return "", "", false
	}
	return string(canonicalJSON), token, true
}

// OtherSettingsEqualIgnoringBalanceQueryToken reports whether the channel's
// other settings equal origin's ignoring the user_api balance-query access
// token, but only while that token is itself unchanged: the sole caller
// (UpdateChannel) runs RestoreBalanceQueryAccessToken first, so a redacted
// round-trip carries the stored token and compares equal, while a supplied
// different token is a credential change (like the channel key) and reports
// unequal so it keeps requiring ChannelSensitiveWrite.
//
// Fail-closed semantics: unparseable settings on either side compare unequal
// (treated as a change), preserving the legacy byte-inequality behavior for
// data we cannot reason about.
func (channel *Channel) OtherSettingsEqualIgnoringBalanceQueryToken(origin *Channel) bool {
	left, leftToken, leftOK := channel.canonicalizeOtherSettingsForCompare()
	right, rightToken, rightOK := origin.canonicalizeOtherSettingsForCompare()
	if !leftOK || !rightOK {
		return false
	}
	if leftToken != rightToken {
		return false
	}
	return left == right
}

// StripBalanceQuerySettings removes the balance_query block from the channel's
// other settings in place. Balance query settings only apply to New API
// channels, so a stale block (e.g. left behind by a channel type change) is
// stripped on save instead of retaining an unused upstream credential. The
// edit is done on the raw settings JSON so keys unknown to the settings DTO
// survive byte-identically; an unparseable settings string is left untouched.
func (channel *Channel) StripBalanceQuerySettings() {
	if strings.TrimSpace(channel.OtherSettings) == "" {
		return
	}
	settings := map[string]json.RawMessage{}
	if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil || settings == nil {
		return
	}
	changed := false
	fields := []string{}
	gjson.Parse(channel.OtherSettings).ForEach(func(queryKey, value gjson.Result) bool {
		if strings.EqualFold(queryKey.Str, "balance_query") {
			changed = true
			return true
		}
		fields = append(fields, queryKey.Raw+":"+value.Raw)
		return true
	})
	if !changed {
		return
	}
	channel.OtherSettings = "{" + strings.Join(fields, ",") + "}"
}

func (channel *Channel) Delete() error {
	var err error
	err = DB.Delete(channel).Error
	if err != nil {
		return err
	}
	err = channel.DeleteAbilities()
	return err
}

var channelStatusLock sync.Mutex

// channelPollingLocks stores locks for each channel.id to ensure thread-safe polling
var channelPollingLocks sync.Map

// balanceQueryExtractCompileEnvPrototype is the compile-time type-checking
// environment ValidateSettings uses for the New API custom-mode balance
// extract expression. It must mirror the runtime environment built by
// controller.balanceExprEnv: same names, compatible signatures — an
// expression that compiles against this prototype compiles at query time.
var balanceQueryExtractCompileEnvPrototype = map[string]interface{}{
	"response": map[string]interface{}{},
	"json":     func(string) interface{} { return nil },
	"max":      math.Max,
	"min":      math.Min,
	"abs":      math.Abs,
	"ceil":     math.Ceil,
	"floor":    math.Floor,
}

// GetChannelPollingLock returns or creates a mutex for the given channel ID
func GetChannelPollingLock(channelId int) *sync.Mutex {
	if lock, exists := channelPollingLocks.Load(channelId); exists {
		return lock.(*sync.Mutex)
	}
	// Create new lock for this channel
	newLock := &sync.Mutex{}
	actual, _ := channelPollingLocks.LoadOrStore(channelId, newLock)
	return actual.(*sync.Mutex)
}

// CleanupChannelPollingLocks removes locks for channels that no longer exist
// This is optional and can be called periodically to prevent memory leaks
func CleanupChannelPollingLocks() {
	var activeChannelIds []int
	DB.Model(&Channel{}).Pluck("id", &activeChannelIds)

	activeChannelSet := make(map[int]bool)
	for _, id := range activeChannelIds {
		activeChannelSet[id] = true
	}

	channelPollingLocks.Range(func(key, value interface{}) bool {
		channelId := key.(int)
		if !activeChannelSet[channelId] {
			channelPollingLocks.Delete(channelId)
		}
		return true
	})
}

func handlerMultiKeyUpdate(channel *Channel, usingKey string, status int, reason string) {
	keys := channel.GetKeys()
	if len(keys) == 0 {
		channel.Status = status
	} else {
		keyIndex := -1
		for i, key := range keys {
			if key == usingKey {
				keyIndex = i
				break
			}
		}
		if keyIndex < 0 {
			if usingKey != "" {
				common.SysLog(fmt.Sprintf("failed to update multi-key status: channel_id=%d, using key not found", channel.Id))
				return
			}
			channel.Status = status
			info := channel.GetOtherInfo()
			info["status_reason"] = reason
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
			return
		}
		if channel.ChannelInfo.MultiKeyStatusList == nil {
			channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		}
		if status == common.ChannelStatusEnabled {
			delete(channel.ChannelInfo.MultiKeyStatusList, keyIndex)
		} else {
			channel.ChannelInfo.MultiKeyStatusList[keyIndex] = status
			if channel.ChannelInfo.MultiKeyDisabledReason == nil {
				channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
			}
			if channel.ChannelInfo.MultiKeyDisabledTime == nil {
				channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
			}
			channel.ChannelInfo.MultiKeyDisabledReason[keyIndex] = reason
			channel.ChannelInfo.MultiKeyDisabledTime[keyIndex] = common.GetTimestamp()
		}
		if !hasEnabledMultiKey(keys, channel.ChannelInfo.MultiKeyStatusList) {
			channel.Status = common.ChannelStatusAutoDisabled
			info := channel.GetOtherInfo()
			info["status_reason"] = "All keys are disabled"
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
		} else if status == common.ChannelStatusEnabled {
			channel.Status = common.ChannelStatusEnabled
		}
	}
}

func hasEnabledMultiKey(keys []string, statusList map[int]int) bool {
	for i := range keys {
		if statusList == nil {
			return true
		}
		status, ok := statusList[i]
		if !ok || status == common.ChannelStatusEnabled {
			return true
		}
	}
	return false
}

func UpdateChannelStatus(channelId int, usingKey string, status int, reason string) bool {
	if common.MemoryCacheEnabled {
		channelStatusLock.Lock()
		defer channelStatusLock.Unlock()
	}

	// ChannelInfo stores both multi-key status and the polling cursor. Hold the
	// same per-channel lock from the first read through persistence so neither
	// writer can save a stale JSON snapshot over the other.
	pollingLock := GetChannelPollingLock(channelId)
	pollingLock.Lock()
	defer pollingLock.Unlock()

	if common.MemoryCacheEnabled {
		channelCache, _ := CacheGetChannel(channelId)
		if channelCache == nil {
			return false
		}
		if channelCache.ChannelInfo.IsMultiKey {
			beforeStatus := channelCache.Status
			// 如果是多Key模式，更新缓存中的状态
			handlerMultiKeyUpdate(channelCache, usingKey, status, reason)
			if beforeStatus != channelCache.Status {
				CacheUpdateChannelStatus(channelId, channelCache.Status)
			}
			//CacheUpdateChannel(channelCache)
			//return true
		} else {
			// 如果缓存渠道存在，且状态已是目标状态，直接返回
			if channelCache.Status == status {
				return false
			}
			CacheUpdateChannelStatus(channelId, status)
		}
	}

	shouldUpdateAbilities := false
	defer func() {
		if shouldUpdateAbilities {
			err := UpdateAbilityStatus(channelId, status == common.ChannelStatusEnabled)
			if err != nil {
				common.SysLog(fmt.Sprintf("failed to update ability status: channel_id=%d, error=%v", channelId, err))
			}
		}
	}()
	channel, err := GetChannelById(channelId, true)
	if err != nil {
		return false
	} else {
		if channel.Status == status {
			return false
		}

		if channel.ChannelInfo.IsMultiKey {
			beforeStatus := channel.Status
			handlerMultiKeyUpdate(channel, usingKey, status, reason)
			if beforeStatus != channel.Status {
				shouldUpdateAbilities = true
			}
		} else {
			info := channel.GetOtherInfo()
			info["status_reason"] = reason
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
			channel.Status = status
			shouldUpdateAbilities = true
		}
		err = channel.saveStatusState()
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to update channel status: channel_id=%d, status=%d, error=%v", channel.Id, status, err))
			return false
		}
	}
	return true
}

func EnableChannelByTag(tag string) error {
	err := DB.Model(&Channel{}).Where("tag = ?", tag).Update("status", common.ChannelStatusEnabled).Error
	if err != nil {
		return err
	}
	err = UpdateAbilityStatusByTag(tag, true)
	return err
}

func DisableChannelByTag(tag string) error {
	err := DB.Model(&Channel{}).Where("tag = ?", tag).Update("status", common.ChannelStatusManuallyDisabled).Error
	if err != nil {
		return err
	}
	err = UpdateAbilityStatusByTag(tag, false)
	return err
}

func EditChannelByTag(tag string, newTag *string, modelMapping *string, models *string, group *string, priority *int64, weight *uint, paramOverride *string, headerOverride *string) error {
	updateData := Channel{}
	shouldReCreateAbilities := false
	updatedTag := tag
	// 如果 newTag 不为空且不等于 tag，则更新 tag
	if newTag != nil && *newTag != tag {
		updateData.Tag = newTag
		updatedTag = *newTag
	}
	if modelMapping != nil {
		updateData.ModelMapping = modelMapping
	}
	if models != nil && *models != "" {
		shouldReCreateAbilities = true
		updateData.Models = *models
	}
	if group != nil && *group != "" {
		shouldReCreateAbilities = true
		updateData.Group = *group
	}
	if priority != nil {
		updateData.Priority = priority
	}
	if weight != nil {
		updateData.Weight = weight
	}
	if paramOverride != nil {
		updateData.ParamOverride = paramOverride
	}
	if headerOverride != nil {
		updateData.HeaderOverride = headerOverride
	}

	err := DB.Model(&Channel{}).Where("tag = ?", tag).Updates(updateData).Error
	if err != nil {
		return err
	}
	if shouldReCreateAbilities {
		channels, err := GetChannelsByTag(updatedTag, false, false)
		if err == nil {
			for _, channel := range channels {
				err = channel.UpdateAbilities(nil)
				if err != nil {
					common.SysLog(fmt.Sprintf("failed to update abilities: channel_id=%d, tag=%s, error=%v", channel.Id, channel.GetTag(), err))
				}
			}
		}
	} else {
		err := UpdateAbilityByTag(tag, newTag, priority, weight)
		if err != nil {
			return err
		}
	}
	return nil
}

func UpdateChannelUsedQuota(id int, quota int) {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeChannelUsedQuota, id, quota)
		return
	}
	updateChannelUsedQuota(id, quota)
}

func updateChannelUsedQuota(id int, quota int) {
	err := DB.Model(&Channel{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update channel used quota: channel_id=%d, delta_quota=%d, error=%v", id, quota, err))
	}
}

func DeleteChannelByStatus(status int64) (int64, error) {
	result := DB.Where("status = ?", status).Delete(&Channel{})
	return result.RowsAffected, result.Error
}

func DeleteDisabledChannel() (int64, error) {
	result := DB.Where("status = ? or status = ?", common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled).Delete(&Channel{})
	return result.RowsAffected, result.Error
}

func GetPaginatedTags(offset int, limit int) ([]*string, error) {
	return GetPaginatedChannelTags(DB.Model(&Channel{}), offset, limit)
}

func GetPaginatedChannelTags(query *gorm.DB, offset int, limit int) ([]*string, error) {
	var tags []*string
	err := query.
		Select("DISTINCT tag").
		Where("tag is not null AND tag != ''").
		Order(clause.OrderByColumn{Column: clause.Column{Name: "tag"}}).
		Offset(offset).
		Limit(limit).
		Find(&tags).Error
	return tags, err
}

func SearchTags(keyword string, group string, model string, idSort bool) ([]*string, error) {
	var tags []*string
	modelsCol := "`models`"

	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// 如果是 PostgreSQL，使用双引号
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	order := "priority desc"
	if idSort {
		order = "id desc"
	}

	// 构造基础查询
	baseQuery := DB.Model(&Channel{}).Omit("key")

	// 构造WHERE子句
	whereClause := "(id = ? OR name LIKE ? OR " + commonKeyCol + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	baseQuery = ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)

	subQuery := baseQuery.
		Select("tag").
		Where("tag != ''").
		Order(order)

	err := DB.Table("(?) as sub", subQuery).
		Select("DISTINCT tag").
		Find(&tags).Error

	if err != nil {
		return nil, err
	}

	return tags, nil
}

func (channel *Channel) ValidateSettings() error {
	channelParams := &dto.ChannelSettings{}
	if channel.Setting != nil && *channel.Setting != "" {
		err := common.Unmarshal([]byte(*channel.Setting), channelParams)
		if err != nil {
			return err
		}
	}
	if _, err := common.ParseProxyURLStrict(channelParams.Proxy); err != nil {
		return fmt.Errorf("invalid channel proxy: %w", err)
	}
	if err := channelParams.ValidateHTTPTransport(); err != nil {
		return err
	}
	channelOtherSettings := &dto.ChannelOtherSettings{}
	if channel.OtherSettings != "" {
		err := common.UnmarshalJsonStr(channel.OtherSettings, channelOtherSettings)
		if err != nil {
			return err
		}
	}
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		if channelOtherSettings.AdvancedCustom == nil {
			return fmt.Errorf("advanced_custom is required")
		}
	}
	if channelOtherSettings.AdvancedCustom != nil {
		if err := channelOtherSettings.AdvancedCustom.Validate(); err != nil {
			return err
		}
	}
	if channel.Type == constant.ChannelTypeNewAPI {
		if err := channelOtherSettings.BalanceQuery.Validate(); err != nil {
			return err
		}
		// Save-time and runtime compilation share the same restrictions so
		// unsafe extract expressions cannot be stored or executed.
		if channelOtherSettings.BalanceQuery != nil &&
			channelOtherSettings.BalanceQuery.NormalizedMode() == dto.BalanceQueryModeCustom &&
			strings.TrimSpace(channelOtherSettings.BalanceQuery.Extract) != "" {
			if _, err := common.CompileBalanceQueryExtract(channelOtherSettings.BalanceQuery.Extract,
				balanceQueryExtractCompileEnvPrototype); err != nil {
				return fmt.Errorf("balance_query.extract is not a valid expression: %w", err)
			}
		}
		if channelOtherSettings.BalanceQuery.UsesRelativeURL() &&
			strings.TrimSpace(channel.GetBaseURL()) == "" {
			return fmt.Errorf("channel base URL is required when the balance query URL is relative")
		}
	}
	if channel.Type == constant.ChannelTypeAdvancedCustom && channelOtherSettings.UpstreamModelUpdateCheckEnabled {
		if _, ok := channelOtherSettings.AdvancedCustom.ModelListRoute(); !ok {
			return fmt.Errorf("advanced custom channels require a %s route when upstream model update checks are enabled", dto.AdvancedCustomModelListPath)
		}
	}
	// Balance query settings only apply to New API channels; strip stale
	// blocks (e.g. left behind by a channel type change) so an unused
	// upstream credential is never retained on save.
	if channel.Type != constant.ChannelTypeNewAPI {
		channel.StripBalanceQuerySettings()
	}
	return nil
}

func (channel *Channel) GetSetting() dto.ChannelSettings {
	setting := dto.ChannelSettings{}
	if channel.Setting != nil && *channel.Setting != "" {
		err := common.Unmarshal([]byte(*channel.Setting), &setting)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal setting: channel_id=%d, error=%v", channel.Id, err))
			channel.Setting = nil // 清空设置以避免后续错误
			_ = channel.Save()    // 保存修改
		}
	}
	return setting
}

func (channel *Channel) SetSetting(setting dto.ChannelSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.Setting = common.GetPointer[string](string(settingBytes))
}

func (channel *Channel) GetOtherSettings() dto.ChannelOtherSettings {
	setting := dto.ChannelOtherSettings{}
	if channel.OtherSettings != "" {
		err := common.UnmarshalJsonStr(channel.OtherSettings, &setting)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal setting: channel_id=%d, error=%v", channel.Id, err))
			channel.OtherSettings = "{}" // 清空设置以避免后续错误
			_ = channel.Save()           // 保存修改
		}
	}
	return setting
}

func (channel *Channel) SetOtherSettings(setting dto.ChannelOtherSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.OtherSettings = string(settingBytes)
}

func (channel *Channel) GetParamOverride() map[string]interface{} {
	paramOverride := make(map[string]interface{})
	if channel.ParamOverride != nil && *channel.ParamOverride != "" {
		err := common.Unmarshal([]byte(*channel.ParamOverride), &paramOverride)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal param override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return paramOverride
}

func (channel *Channel) GetHeaderOverride() map[string]interface{} {
	headerOverride := make(map[string]interface{})
	if channel.HeaderOverride != nil && *channel.HeaderOverride != "" {
		err := common.Unmarshal([]byte(*channel.HeaderOverride), &headerOverride)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal header override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return headerOverride
}

func GetChannelsByIds(ids []int) ([]*Channel, error) {
	var channels []*Channel
	err := DB.Where("id in (?)", ids).Find(&channels).Error
	return channels, err
}

func BatchSetChannelTag(ids []int, tag *string) error {
	// 开启事务
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	// 更新标签
	err := tx.Model(&Channel{}).Where("id in (?)", ids).Update("tag", tag).Error
	if err != nil {
		tx.Rollback()
		return err
	}

	// update ability status
	channels, err := GetChannelsByIds(ids)
	if err != nil {
		tx.Rollback()
		return err
	}

	for _, channel := range channels {
		err = channel.UpdateAbilities(tx)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	// 提交事务
	return tx.Commit().Error
}

// CountAllChannels returns total channels in DB
func CountAllChannels() (int64, error) {
	var total int64
	err := DB.Model(&Channel{}).Count(&total).Error
	return total, err
}

// CountAllTags returns number of non-empty distinct tags
func CountAllTags() (int64, error) {
	return CountChannelTags(DB.Model(&Channel{}))
}

func CountChannelTags(query *gorm.DB) (int64, error) {
	var total int64
	err := query.Where("tag is not null AND tag != ''").Distinct("tag").Count(&total).Error
	return total, err
}

// Get channels of specified type with pagination
func GetChannelsByType(startIdx int, num int, idSort bool, channelType int) ([]*Channel, error) {
	var channels []*Channel
	order := "priority desc"
	if idSort {
		order = "id desc"
	}
	err := DB.Where("type = ?", channelType).Order(order).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	return channels, err
}

// Count channels of specific type
func CountChannelsByType(channelType int) (int64, error) {
	var count int64
	err := DB.Model(&Channel{}).Where("type = ?", channelType).Count(&count).Error
	return count, err
}

// Return map[type]count for all channels
func CountChannelsGroupByType() (map[int64]int64, error) {
	type result struct {
		Type  int64 `gorm:"column:type"`
		Count int64 `gorm:"column:count"`
	}
	var results []result
	err := DB.Model(&Channel{}).Select("type, count(*) as count").Group("type").Find(&results).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int64)
	for _, r := range results {
		counts[r.Type] = r.Count
	}
	return counts, nil
}

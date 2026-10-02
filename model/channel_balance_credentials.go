package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/tidwall/gjson"
)

// balanceQueryRequestCredentials replaces custom request values with an opaque
// marker, or restores markers from the saved request. All request values may
// carry credentials, including arbitrary header names, URL paths, and bodies.
// Preserve JSON order and aliases because the settings DTO merges them in order.
func balanceQueryRequestCredentials(settings string, savedSettings *string) string {
	type queryObject struct {
		name  string
		value gjson.Result
	}
	savedQueries := []queryObject{}
	fallback := gjson.Result{}
	if savedSettings != nil {
		var saved dto.ChannelOtherSettings
		if err := common.UnmarshalJsonStr(*savedSettings, &saved); err != nil ||
			saved.BalanceQuery.NormalizedMode() != dto.BalanceQueryModeCustom {
			return settings
		}
		encoded, err := common.Marshal(saved.BalanceQuery)
		if err != nil {
			return settings
		}
		fallback = gjson.ParseBytes(encoded)
		gjson.Parse(*savedSettings).ForEach(func(name, value gjson.Result) bool {
			if strings.EqualFold(name.Str, "balance_query") {
				savedQueries = append(savedQueries, queryObject{name.Str, value})
			}
			return true
		})
	}
	queryNames := []string{}
	gjson.Parse(settings).ForEach(func(name, value gjson.Result) bool {
		if strings.EqualFold(name.Str, "balance_query") {
			queryNames = append(queryNames, name.Str)
		}
		return true
	})
	preserveAliases := len(savedQueries) == len(queryNames)
	for index, name := range queryNames {
		if preserveAliases && savedQueries[index].name != name {
			preserveAliases = false
		}
	}
	marker, _ := common.Marshal(dto.RedactedBalanceQueryValue)
	queryIndex := 0
	fields := []string{}
	gjson.Parse(settings).ForEach(func(name, value gjson.Result) bool {
		raw := value.Raw
		if strings.EqualFold(name.Str, "balance_query") {
			savedQuery := fallback
			if preserveAliases {
				savedQuery = savedQueries[queryIndex].value
			}
			queryIndex++
			if value.IsObject() {
				savedFields := map[string][]gjson.Result{}
				savedQuery.ForEach(func(key, field gjson.Result) bool {
					savedFields[key.Str] = append(savedFields[key.Str], field)
					return true
				})
				queryFields := []string{}
				value.ForEach(func(key, field gjson.Result) bool {
					fieldRaw := field.Raw
					savedField := gjson.Result{}
					if candidates := savedFields[key.Str]; len(candidates) > 0 {
						savedField = candidates[0]
						savedFields[key.Str] = candidates[1:]
					} else {
						savedQuery.ForEach(func(savedKey, savedValue gjson.Result) bool {
							if strings.EqualFold(savedKey.Str, key.Str) {
								savedField = savedValue
							}
							return true
						})
					}
					if strings.EqualFold(key.Str, "url") || strings.EqualFold(key.Str, "body") {
						if savedSettings == nil && field.Type != gjson.Null && fieldRaw != `""` {
							fieldRaw = string(marker)
						} else if field.Type == gjson.String && field.Str == dto.RedactedBalanceQueryValue && savedField.Exists() {
							fieldRaw = savedField.Raw
						}
					} else if strings.EqualFold(key.Str, "headers") && field.IsObject() {
						savedHeaders := map[string][]gjson.Result{}
						savedField.ForEach(func(header, headerValue gjson.Result) bool {
							savedHeaders[header.Str] = append(savedHeaders[header.Str], headerValue)
							return true
						})
						headers := []string{}
						field.ForEach(func(header, headerValue gjson.Result) bool {
							headerRaw := headerValue.Raw
							candidates := savedHeaders[header.Str]
							if savedSettings == nil && headerRaw != `""` && headerValue.Type != gjson.Null {
								headerRaw = string(marker)
							} else if headerValue.Type == gjson.String && headerValue.Str == dto.RedactedBalanceQueryValue && len(candidates) > 0 {
								headerRaw = candidates[0].Raw
							}
							if len(candidates) > 0 {
								savedHeaders[header.Str] = candidates[1:]
							}
							headers = append(headers, header.Raw+":"+headerRaw)
							return true
						})
						fieldRaw = "{" + strings.Join(headers, ",") + "}"
					} else if strings.EqualFold(key.Str, "headers") && savedSettings == nil && field.Type != gjson.Null {
						fieldRaw = "{}"
					}
					queryFields = append(queryFields, key.Raw+":"+fieldRaw)
					return true
				})
				raw = "{" + strings.Join(queryFields, ",") + "}"
			}
		}
		fields = append(fields, name.Raw+":"+raw)
		return true
	})
	return "{" + strings.Join(fields, ",") + "}"
}

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/expr-lang/expr"
	"github.com/tidwall/gjson"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/advancedcustom"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"

	"github.com/gin-gonic/gin"
)

// https://github.com/songquanpeng/one-api/issues/79

type OpenAISubscriptionResponse struct {
	Object             string  `json:"object"`
	HasPaymentMethod   bool    `json:"has_payment_method"`
	SoftLimitUSD       float64 `json:"soft_limit_usd"`
	HardLimitUSD       float64 `json:"hard_limit_usd"`
	SystemHardLimitUSD float64 `json:"system_hard_limit_usd"`
	AccessUntil        int64   `json:"access_until"`
}

type OpenAIUsageDailyCost struct {
	Timestamp float64 `json:"timestamp"`
	LineItems []struct {
		Name string  `json:"name"`
		Cost float64 `json:"cost"`
	}
}

type OpenAICreditGrants struct {
	Object         string  `json:"object"`
	TotalGranted   float64 `json:"total_granted"`
	TotalUsed      float64 `json:"total_used"`
	TotalAvailable float64 `json:"total_available"`
}

const maxAdvancedCustomBalanceResponseBytes = 256 << 10

// balanceQueryRequestTimeout bounds a single balance-query HTTP round trip.
// It deliberately ignores RELAY_TIMEOUT: that setting defaults to 0 (no
// timeout) to protect long streaming relay calls, while a balance query is a
// short request whose failure must not stall the serial
// updateAllChannelsBalance loop. It is a variable so tests can shrink it.
var balanceQueryRequestTimeout = 30 * time.Second

type channelBalanceResult struct {
	Balance     float64
	RawResponse string
}

type OpenAIUsageResponse struct {
	Object string `json:"object"`
	//DailyCosts []OpenAIUsageDailyCost `json:"daily_costs"`
	TotalUsage float64 `json:"total_usage"` // unit: 0.01 dollar
}

type OpenAISBUsageResponse struct {
	Msg  string `json:"msg"`
	Data *struct {
		Credit string `json:"credit"`
	} `json:"data"`
}

type AIProxyUserOverviewResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	ErrorCode int    `json:"error_code"`
	Data      struct {
		TotalPoints float64 `json:"totalPoints"`
	} `json:"data"`
}

type API2GPTUsageResponse struct {
	Object         string  `json:"object"`
	TotalGranted   float64 `json:"total_granted"`
	TotalUsed      float64 `json:"total_used"`
	TotalRemaining float64 `json:"total_remaining"`
}

type APGC2DGPTUsageResponse struct {
	//Grants         interface{} `json:"grants"`
	Object         string  `json:"object"`
	TotalAvailable float64 `json:"total_available"`
	TotalGranted   float64 `json:"total_granted"`
	TotalUsed      float64 `json:"total_used"`
}

type SiliconFlowUsageResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  bool   `json:"status"`
	Data    struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Image         string `json:"image"`
		Email         string `json:"email"`
		IsAdmin       bool   `json:"isAdmin"`
		Balance       string `json:"balance"`
		Status        string `json:"status"`
		Introduction  string `json:"introduction"`
		Role          string `json:"role"`
		ChargeBalance string `json:"chargeBalance"`
		TotalBalance  string `json:"totalBalance"`
		Category      string `json:"category"`
	} `json:"data"`
}

type DeepSeekUsageResponse struct {
	IsAvailable  bool `json:"is_available"`
	BalanceInfos []struct {
		Currency        string `json:"currency"`
		TotalBalance    string `json:"total_balance"`
		GrantedBalance  string `json:"granted_balance"`
		ToppedUpBalance string `json:"topped_up_balance"`
	} `json:"balance_infos"`
}

type OpenRouterCreditResponse struct {
	Data struct {
		TotalCredits float64 `json:"total_credits"`
		TotalUsage   float64 `json:"total_usage"`
	} `json:"data"`
}

// GetAuthHeader get auth header
func GetAuthHeader(token string) http.Header {
	h := http.Header{}
	h.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	return h
}

// GetClaudeAuthHeader get claude auth header
func GetClaudeAuthHeader(token string) http.Header {
	h := http.Header{}
	h.Add("x-api-key", token)
	h.Add("anthropic-version", "2023-06-01")
	return h
}

func GetResponseBody(method, url string, channel *model.Channel, headers http.Header) ([]byte, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, balanceQueryHTTPError(err)
	}
	for k := range headers {
		req.Header.Add(k, headers.Get(k))
	}
	client, err := service.GetHttpClientWithProxy(channel.GetSetting().Proxy)
	if err != nil {
		return nil, balanceQueryHTTPError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), balanceQueryRequestTimeout)
	defer cancel()
	res, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, balanceQueryHTTPError(err)
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		return nil, fmt.Errorf("status code: %d", res.StatusCode)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxAdvancedCustomBalanceResponseBytes+1))
	if err != nil {
		return nil, balanceQueryHTTPError(err)
	}
	if len(body) > maxAdvancedCustomBalanceResponseBytes {
		return nil, fmt.Errorf("balance response exceeds %d bytes", maxAdvancedCustomBalanceResponseBytes)
	}
	return body, nil
}

func updateChannelCloseAIBalance(channel *model.Channel) (float64, error) {
	url := fmt.Sprintf("%s/dashboard/billing/credit_grants", channel.GetBaseURL())
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))

	if err != nil {
		return 0, err
	}
	response := OpenAICreditGrants{}
	err = common.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	channel.UpdateBalance(response.TotalAvailable)
	return response.TotalAvailable, nil
}

func updateChannelOpenAISBBalance(channel *model.Channel) (float64, error) {
	url := fmt.Sprintf("https://api.openai-sb.com/sb-api/user/status?api_key=%s", channel.Key)
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	response := OpenAISBUsageResponse{}
	err = common.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	if response.Data == nil {
		return 0, errors.New(response.Msg)
	}
	balance, err := strconv.ParseFloat(response.Data.Credit, 64)
	if err != nil {
		return 0, err
	}
	channel.UpdateBalance(balance)
	return balance, nil
}

func updateChannelAIProxyBalance(channel *model.Channel) (float64, error) {
	url := "https://aiproxy.io/api/report/getUserOverview"
	headers := http.Header{}
	headers.Add("Api-Key", channel.Key)
	body, err := GetResponseBody("GET", url, channel, headers)
	if err != nil {
		return 0, err
	}
	response := AIProxyUserOverviewResponse{}
	err = common.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	if !response.Success {
		return 0, fmt.Errorf("code: %d, message: %s", response.ErrorCode, response.Message)
	}
	channel.UpdateBalance(response.Data.TotalPoints)
	return response.Data.TotalPoints, nil
}

func updateChannelAPI2GPTBalance(channel *model.Channel) (float64, error) {
	url := "https://api.api2gpt.com/dashboard/billing/credit_grants"
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))

	if err != nil {
		return 0, err
	}
	response := API2GPTUsageResponse{}
	err = common.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	channel.UpdateBalance(response.TotalRemaining)
	return response.TotalRemaining, nil
}

func updateChannelSiliconFlowBalance(channel *model.Channel) (float64, error) {
	url := "https://api.siliconflow.cn/v1/user/info"
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	response := SiliconFlowUsageResponse{}
	err = common.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	if response.Code != 20000 {
		return 0, fmt.Errorf("code: %d, message: %s", response.Code, response.Message)
	}
	balance, err := strconv.ParseFloat(response.Data.TotalBalance, 64)
	if err != nil {
		return 0, err
	}
	channel.UpdateBalance(balance)
	return balance, nil
}

func updateChannelDeepSeekBalance(channel *model.Channel) (float64, error) {
	url := "https://api.deepseek.com/user/balance"
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	response := DeepSeekUsageResponse{}
	err = common.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	index := -1
	for i, balanceInfo := range response.BalanceInfos {
		if balanceInfo.Currency == "CNY" {
			index = i
			break
		}
	}
	if index == -1 {
		return 0, errors.New("currency CNY not found")
	}
	balance, err := strconv.ParseFloat(response.BalanceInfos[index].TotalBalance, 64)
	if err != nil {
		return 0, err
	}
	channel.UpdateBalance(balance)
	return balance, nil
}

func updateChannelAIGC2DBalance(channel *model.Channel) (float64, error) {
	url := "https://api.aigc2d.com/dashboard/billing/credit_grants"
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	response := APGC2DGPTUsageResponse{}
	err = common.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	channel.UpdateBalance(response.TotalAvailable)
	return response.TotalAvailable, nil
}

func updateChannelOpenRouterBalance(channel *model.Channel) (float64, error) {
	url := "https://openrouter.ai/api/v1/credits"
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	response := OpenRouterCreditResponse{}
	err = common.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	balance := response.Data.TotalCredits - response.Data.TotalUsage
	channel.UpdateBalance(balance)
	return balance, nil
}

func updateChannelMoonshotBalance(channel *model.Channel) (float64, error) {
	url := "https://api.moonshot.cn/v1/users/me/balance"
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}

	type MoonshotBalanceData struct {
		AvailableBalance float64 `json:"available_balance"`
		VoucherBalance   float64 `json:"voucher_balance"`
		CashBalance      float64 `json:"cash_balance"`
	}

	type MoonshotBalanceResponse struct {
		Code   int                 `json:"code"`
		Data   MoonshotBalanceData `json:"data"`
		Scode  string              `json:"scode"`
		Status bool                `json:"status"`
	}

	response := MoonshotBalanceResponse{}
	err = common.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	if !response.Status || response.Code != 0 {
		return 0, fmt.Errorf("failed to update moonshot balance, status: %v, code: %d, scode: %s", response.Status, response.Code, response.Scode)
	}
	availableBalanceCny := response.Data.AvailableBalance
	availableBalanceUsd := decimal.NewFromFloat(availableBalanceCny).Div(decimal.NewFromFloat(operation_setting.Price)).InexactFloat64()
	channel.UpdateBalance(availableBalanceUsd)
	return availableBalanceUsd, nil
}

func fetchAdvancedCustomBalance(channel *model.Channel) (channelBalanceResult, error) {
	key := strings.TrimSpace(channel.Key)
	info := &relaycommon.RelayInfo{
		RelayFormat:    types.RelayFormatOpenAI,
		RelayMode:      relayconstant.RelayModeUnknown,
		RequestURLPath: dto.AdvancedCustomBalancePath,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeAdvancedCustom,
			ChannelBaseUrl:       channel.GetBaseURL(),
			ApiKey:               key,
			ChannelOtherSettings: channel.GetOtherSettings(),
		},
	}
	requestURL, headers, err := (&advancedcustom.Adaptor{}).BuildBalanceRequest(info)
	if err != nil {
		return channelBalanceResult{}, sanitizeFetchModelsError(err, key)
	}
	if err := applyFetchModelsHeaderOverrides(channel, key, headers); err != nil {
		return channelBalanceResult{}, sanitizeFetchModelsError(err, key)
	}

	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return channelBalanceResult{}, sanitizeFetchModelsError(err, key)
	}
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
		if strings.EqualFold(name, "Host") {
			request.Host = headers.Get(name)
		}
	}
	client, err := service.GetHttpClientWithProxy(channel.GetSetting().Proxy)
	if err != nil {
		return channelBalanceResult{}, sanitizeFetchModelsError(err, key)
	}
	response, err := client.Do(request)
	if err != nil {
		return channelBalanceResult{}, sanitizeAdvancedCustomRequestError(err, key, requestURL)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return channelBalanceResult{}, fmt.Errorf("status code: %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAdvancedCustomBalanceResponseBytes+1))
	if err != nil {
		return channelBalanceResult{}, sanitizeAdvancedCustomRequestError(err, key, requestURL)
	}
	if len(body) > maxAdvancedCustomBalanceResponseBytes {
		return channelBalanceResult{}, fmt.Errorf("balance response exceeds %d bytes", maxAdvancedCustomBalanceResponseBytes)
	}

	var validated json.RawMessage
	if err := common.Unmarshal(body, &validated); err != nil {
		return channelBalanceResult{}, fmt.Errorf("invalid balance JSON response: %w", err)
	}
	if common.GetJsonType(validated) == "object" {
		var creditSummary struct {
			Object         string          `json:"object"`
			TotalAvailable json.RawMessage `json:"total_available"`
		}
		if err := common.Unmarshal(body, &creditSummary); err != nil {
			return channelBalanceResult{}, fmt.Errorf("invalid balance JSON response: %w", err)
		}
		if creditSummary.Object == "credit_summary" &&
			common.GetJsonType(creditSummary.TotalAvailable) == "number" {
			var balance float64
			if err := common.Unmarshal(creditSummary.TotalAvailable, &balance); err == nil &&
				balance >= 0 &&
				!math.IsNaN(balance) &&
				!math.IsInf(balance, 0) {
				channel.UpdateBalance(balance)
				return channelBalanceResult{Balance: balance}, nil
			}
		}
	}

	formatted, err := common.IndentJson(body)
	if err != nil {
		return channelBalanceResult{}, fmt.Errorf("invalid balance JSON response: %w", err)
	}
	return channelBalanceResult{RawResponse: string(formatted)}, nil
}

func updateChannelBalance(channel *model.Channel) (channelBalanceResult, error) {
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		return fetchAdvancedCustomBalance(channel)
	}
	balance, err := updateStandardChannelBalance(channel)
	return channelBalanceResult{Balance: balance}, err
}

// updateNewAPIChannelBalance resolves the balance for New API channels using
// the channel's balance_query settings (user_api / custom). It returns
// handled=false for subscription mode (the default), which keeps the shared
// OpenAI-compatible dashboard flow in updateStandardChannelBalance.
func updateNewAPIChannelBalance(channel *model.Channel) (bool, float64, error) {
	balanceQuery := channel.GetOtherSettings().BalanceQuery
	key := strings.TrimSpace(channel.Key)
	switch balanceQuery.NormalizedMode() {
	case dto.BalanceQueryModeUserAPI:
		balance, err := fetchNewAPIUserAPIBalance(channel, balanceQuery, key)
		return true, balance, err
	case dto.BalanceQueryModeCustom:
		balance, err := fetchCustomTemplateBalance(channel, balanceQuery, key)
		return true, balance, err
	default:
		return false, 0, nil
	}
}

// fetchNewAPIUserAPIBalance queries the upstream New API dashboard endpoint
// GET {base_url}/api/user/self with the configured access token and user id,
// mirroring the cc-switch "New API" usage-query template.
func fetchNewAPIUserAPIBalance(channel *model.Channel, balanceQuery *dto.ChannelBalanceQuery, key string) (float64, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
	if baseURL == "" {
		return 0, errors.New("base URL is required for user_api balance queries")
	}
	accessToken := strings.TrimSpace(balanceQuery.AccessToken)
	if accessToken == "" {
		// Settings written before save-time validation existed can carry an
		// empty token; fail with a clear message instead of sending a bare
		// "Bearer " header to the upstream.
		return 0, errors.New("balance query access token is missing; re-save the channel with a new access token")
	}
	userId := strings.TrimSpace(balanceQuery.UserId)
	if userId == "" {
		// Same legacy-settings gap as the token: fail locally instead of
		// sending an empty New-Api-User header that only yields a generic
		// upstream rejection.
		return 0, errors.New("balance query user id is missing; re-save the channel with a user id")
	}
	// Match the custom-mode guard: reject a base URL that is not an http(s)
	// origin so a legacy or hand-edited channel cannot smuggle the query to a
	// non-HTTP scheme or yield a confusing transport error.
	parsedBase, err := url.Parse(baseURL)
	if err != nil || parsedBase.Scheme == "" || parsedBase.Host == "" ||
		(!strings.EqualFold(parsedBase.Scheme, "http") && !strings.EqualFold(parsedBase.Scheme, "https")) {
		return 0, errors.New("base URL must be a full http(s) URL for user_api balance queries")
	}
	requestURL := baseURL + "/api/user/self"
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, sanitizeFetchModelsError(err, key)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("New-Api-User", userId)
	request.Header.Set("Content-Type", "application/json")

	body, err := executeBalanceRequest(channel, request, key)
	if err != nil {
		return 0, err
	}
	response := struct {
		Success bool `json:"success"`
		Data    *struct {
			// float64 (not int64): an upstream that reports a fractional
			// quota must not fail the whole query at unmarshal time.
			Quota *float64 `json:"quota"`
		} `json:"data"`
	}{}
	if err := common.Unmarshal(body, &response); err != nil {
		return 0, fmt.Errorf("invalid user balance JSON response: %w", err)
	}
	if !response.Success || response.Data == nil {
		return 0, fmt.Errorf("upstream user balance query failed (success=%v)", response.Success)
	}
	if response.Data.Quota == nil {
		return 0, errors.New("upstream user balance response is missing quota")
	}
	quotaPerUnit := balanceQuery.QuotaPerUnit
	if quotaPerUnit <= 0 {
		quotaPerUnit = dto.DefaultQuotaPerUnit
	}
	balance := *response.Data.Quota / quotaPerUnit
	if math.IsNaN(balance) || math.IsInf(balance, 0) {
		return 0, fmt.Errorf("user_api balance query returned a non-finite value (quota=%g, quota_per_unit=%g)",
			*response.Data.Quota, quotaPerUnit)
	}
	channel.UpdateBalance(balance)
	return balance, nil
}

// fetchCustomTemplateBalance runs the fully custom balance-query template:
// a configurable method/url/headers/body request against the upstream, with
// an expr expression extracting the numeric balance from the JSON response.
//
// Security trade-off (deliberate): the URL/headers/body support {key}/{base_url}
// placeholders, so a ChannelSensitiveWrite admin can point the query at an
// external host and have the server send the channel key there, and the
// server-side request can reach internal addresses (same SSRF surface as the
// channel base URL itself, which is also admin-controlled). This matches the
// existing advanced_custom balance route, which shares this exposure; the
// channel key therefore remains gated by root-only /channel/:id/key, but this
// path is another way a trusted admin can exfiltrate it. If that needs to
// change, restrict custom URLs to the channel base URL's host here.
//
// Credentials travel only where the template explicitly places them: no
// Authorization header is ever added implicitly, so pointing the query at a
// third-party host never sends the channel key unless the template says so.
func fetchCustomTemplateBalance(channel *model.Channel, balanceQuery *dto.ChannelBalanceQuery, key string) (float64, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
	// Encode query delimiters and use %20 for spaces so the key is safe in
	// both paths and query values. Headers and bodies keep the raw key.
	escapedKey := strings.ReplaceAll(url.QueryEscape(key), "+", "%20")
	rawURL := strings.TrimSpace(applyBalanceTemplatePlaceholders(balanceQuery.URL, baseURL, escapedKey))
	var requestURL *url.URL
	var err error
	if strings.HasPrefix(rawURL, "/") {
		// Relative paths join onto the channel base URL by plain
		// concatenation, preserving any path prefix of the base so
		// "/v1/balance" behaves exactly like the "{base_url}/v1/balance"
		// placeholder form. baseURL is already right-trimmed of "/".
		if baseURL == "" {
			// Save-time validation rejects this combination; keep a clear
			// message here for settings written before that guard existed.
			return 0, fmt.Errorf("channel base URL is required when the balance query URL is relative")
		}
		parsedBase, baseErr := url.Parse(baseURL)
		if baseErr != nil || parsedBase.Scheme == "" || parsedBase.Host == "" ||
			(!strings.EqualFold(parsedBase.Scheme, "http") && !strings.EqualFold(parsedBase.Scheme, "https")) {
			return 0, fmt.Errorf("channel base URL is invalid for a relative balance query URL")
		}
		requestURL, err = url.Parse(baseURL + rawURL)
		if err != nil {
			return 0, fmt.Errorf("balance query URL is invalid after placeholder expansion")
		}
		// Runtime backstop for legacy/hand-edited settings: the joined URL
		// must stay on the channel's own http(s) host. A rawURL starting with
		// "//" would otherwise produce a scheme-relative URL that redirects
		// the balance query (and any credentials the template carries) to an
		// attacker-chosen host.
		if requestURL.Host == "" ||
			(!strings.EqualFold(requestURL.Scheme, "http") && !strings.EqualFold(requestURL.Scheme, "https")) ||
			!strings.EqualFold(requestURL.Host, parsedBase.Host) {
			return 0, fmt.Errorf("balance query URL must stay on the channel base URL host")
		}
	} else {
		requestURL, err = url.Parse(rawURL)
		if err != nil || requestURL.Scheme == "" || requestURL.Host == "" {
			return 0, fmt.Errorf("balance query URL is invalid after placeholder expansion")
		}
	}
	method := strings.ToUpper(strings.TrimSpace(balanceQuery.Method))
	if method == "" {
		method = http.MethodGet
	}
	var requestBody io.Reader
	// Validation rejects GET+body at save time; treat it as a configuration
	// error here too instead of silently dropping the body.
	if strings.TrimSpace(balanceQuery.Body) != "" {
		if method == http.MethodGet {
			return 0, fmt.Errorf("balance query body is only allowed for POST requests")
		}
		requestBody = strings.NewReader(applyBalanceTemplatePlaceholders(balanceQuery.Body, baseURL, key))
	}
	request, err := http.NewRequest(method, requestURL.String(), requestBody)
	if err != nil {
		return 0, sanitizeFetchModelsError(err, key)
	}
	for name, value := range balanceQuery.Headers {
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			continue
		}
		expandedValue := applyBalanceTemplatePlaceholders(value, baseURL, key)
		// "Host" in Header.Set is ignored by net/http; route it to
		// request.Host so virtual-host upstreams actually receive it (same
		// handling as the advanced_custom balance flow).
		if strings.EqualFold(trimmedName, "Host") {
			request.Host = expandedValue
			continue
		}
		request.Header.Set(trimmedName, expandedValue)
	}

	body, err := executeBalanceRequest(channel, request, key)
	if err != nil {
		return 0, err
	}
	balance, err := runBalanceExtractExpr(balanceQuery.Extract, body)
	if err != nil {
		return 0, sanitizeAdvancedCustomRequestError(err, key, requestURL.String())
	}
	if math.IsNaN(balance) || math.IsInf(balance, 0) {
		return 0, fmt.Errorf("balance extract expression returned a non-finite value")
	}
	channel.UpdateBalance(balance)
	return balance, nil
}

// executeBalanceRequest sends the prepared request through the channel proxy
// and returns the bounded response body.
func executeBalanceRequest(channel *model.Channel, request *http.Request, key string) ([]byte, error) {
	client, err := service.GetHttpClientWithProxy(channel.GetSetting().Proxy)
	if err != nil {
		return nil, sanitizeFetchModelsError(err, key)
	}
	// Balance queries are short requests and must not inherit the relay
	// client's timeout: RELAY_TIMEOUT defaults to 0 (no timeout) because
	// streaming relay calls need it, but a hanging balance endpoint would
	// otherwise stall the serial updateAllChannelsBalance loop (and the
	// manual UpdateChannelBalance endpoint) indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), balanceQueryRequestTimeout)
	defer cancel()
	response, err := client.Do(request.WithContext(ctx))
	if err != nil {
		return nil, balanceQueryHTTPError(err)
	}
	defer response.Body.Close()
	// Accept any 2xx: a custom-mode endpoint answering 201 with a JSON body is
	// valid; anything else (or a missing body) fails downstream at JSON parse.
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("status code: %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAdvancedCustomBalanceResponseBytes+1))
	if err != nil {
		return nil, balanceQueryHTTPError(err)
	}
	if len(body) > maxAdvancedCustomBalanceResponseBytes {
		return nil, fmt.Errorf("balance response exceeds %d bytes", maxAdvancedCustomBalanceResponseBytes)
	}
	return body, nil
}

// Upstream errors can quote reflected request credentials, including in a
// malformed redirect Location. Expose only safe timeout/cancellation diagnoses.
func balanceQueryHTTPError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	var timeoutError net.Error
	if errors.As(err, &timeoutError) && timeoutError.Timeout() {
		return context.DeadlineExceeded
	}
	return errors.New("balance query HTTP request failed")
}

// applyBalanceTemplatePlaceholders expands {base_url} and {key} in custom
// balance query templates.
func applyBalanceTemplatePlaceholders(template string, baseURL string, key string) string {
	replaced := strings.ReplaceAll(template, "{base_url}", baseURL)
	return strings.ReplaceAll(replaced, "{key}", key)
}

// runBalanceExtractExpr evaluates the custom-mode extract expression against
// the balance response body. The expression sees the parsed response as
// `response` plus `json("path")` for raw gjson lookups, and must produce a
// number.
func runBalanceExtractExpr(exprStr string, body []byte) (float64, error) {
	trimmedExpr := strings.TrimSpace(exprStr)
	if len(trimmedExpr) > dto.MaxBalanceQueryExtractLength {
		return 0, fmt.Errorf("balance extract expression exceeds %d characters", dto.MaxBalanceQueryExtractLength)
	}
	// No compiled-program cache: the source is bounded by
	// MaxBalanceQueryExtractLength, balance queries are infrequent, and
	// recompiling keeps the code free of eviction bookkeeping.
	program, err := common.CompileBalanceQueryExtract(trimmedExpr, balanceExprEnvPrototype)
	if err != nil {
		return 0, fmt.Errorf("balance extract expression compile error: %w", err)
	}
	var parsedResponse interface{}
	if err := common.Unmarshal(body, &parsedResponse); err != nil {
		return 0, fmt.Errorf("invalid balance JSON response: %w", err)
	}
	env := balanceExprEnv(parsedResponse, func(path string) interface{} {
		result := gjson.GetBytes(body, path)
		if !result.Exists() {
			return nil
		}
		return result.Value()
	})
	out, err := expr.Run(program, env)
	if err != nil {
		// Evaluation errors can quote upstream values, including echoed request
		// credentials. Keep those values out of the balance endpoint response.
		return 0, fmt.Errorf("balance extract expression run error")
	}
	balance, ok := out.(float64)
	if !ok {
		return 0, fmt.Errorf("balance extract expression result is %T, want number", out)
	}
	return balance, nil
}

// balanceExprEnv builds the balance extract expression environment: the parsed
// response as `response`, a `json("path")` lookup, and a small set of numeric
// helpers. The compile-time prototype and the runtime environment must stay
// identical, so both are built from this one definition.
func balanceExprEnv(response interface{}, jsonFunc func(string) interface{}) map[string]interface{} {
	return map[string]interface{}{
		"response": response,
		"json":     jsonFunc,
		"max":      math.Max,
		"min":      math.Min,
		"abs":      math.Abs,
		"ceil":     math.Ceil,
		"floor":    math.Floor,
	}
}

// balanceExprEnvPrototype is the compile-time type-checking environment for
// balance extract expressions.
var balanceExprEnvPrototype = balanceExprEnv(map[string]interface{}{}, func(string) interface{} { return nil })

func updateStandardChannelBalance(channel *model.Channel) (float64, error) {
	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.GetBaseURL() == "" {
		channel.BaseURL = &baseURL
	}
	switch channel.Type {
	case constant.ChannelTypeOpenAI:
		if channel.GetBaseURL() != "" {
			baseURL = channel.GetBaseURL()
		}
	case constant.ChannelTypeAzure:
		return 0, errors.New("尚未实现")
	case constant.ChannelTypeCustom:
		baseURL = channel.GetBaseURL()
	//case common.ChannelTypeOpenAISB:
	//	return updateChannelOpenAISBBalance(channel)
	case constant.ChannelTypeAIProxy:
		return updateChannelAIProxyBalance(channel)
	case constant.ChannelTypeAPI2GPT:
		return updateChannelAPI2GPTBalance(channel)
	case constant.ChannelTypeAIGC2D:
		return updateChannelAIGC2DBalance(channel)
	case constant.ChannelTypeSiliconFlow:
		return updateChannelSiliconFlowBalance(channel)
	case constant.ChannelTypeDeepSeek:
		return updateChannelDeepSeekBalance(channel)
	case constant.ChannelTypeOpenRouter:
		return updateChannelOpenRouterBalance(channel)
	case constant.ChannelTypeMoonshot:
		return updateChannelMoonshotBalance(channel)
	case constant.ChannelTypeNewAPI:
		handled, balance, err := updateNewAPIChannelBalance(channel)
		if handled {
			return balance, err
		}
		// subscription mode keeps the shared OpenAI dashboard flow below,
		// pointed at the channel base URL.
		baseURL = strings.TrimSpace(channel.GetBaseURL())
		if baseURL == "" {
			return 0, errors.New("base URL is required for New API channels to query balance")
		}
	default:
		return 0, errors.New("尚未实现")
	}
	url := fmt.Sprintf("%s/v1/dashboard/billing/subscription", baseURL)

	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	subscription := struct {
		HasPaymentMethod bool            `json:"has_payment_method"`
		HardLimitUSD     *float64        `json:"hard_limit_usd"`
		Error            json.RawMessage `json:"error"`
	}{}
	err = common.Unmarshal(body, &subscription)
	if err != nil {
		return 0, err
	}
	if (len(subscription.Error) != 0 && common.GetJsonType(subscription.Error) != "null") || subscription.HardLimitUSD == nil {
		return 0, errors.New("upstream subscription response failed or is missing hard_limit_usd")
	}
	now := time.Now()
	startDate := fmt.Sprintf("%s-01", now.Format("2006-01"))
	endDate := now.Format("2006-01-02")
	if !subscription.HasPaymentMethod {
		startDate = now.AddDate(0, 0, -100).Format("2006-01-02")
	}
	url = fmt.Sprintf("%s/v1/dashboard/billing/usage?start_date=%s&end_date=%s", baseURL, startDate, endDate)
	body, err = GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	usage := struct {
		TotalUsage *float64        `json:"total_usage"`
		Error      json.RawMessage `json:"error"`
	}{}
	err = common.Unmarshal(body, &usage)
	if err != nil {
		return 0, err
	}
	if (len(usage.Error) != 0 && common.GetJsonType(usage.Error) != "null") || usage.TotalUsage == nil {
		return 0, errors.New("upstream usage response failed or is missing total_usage")
	}
	balance := *subscription.HardLimitUSD - *usage.TotalUsage/100
	if math.IsNaN(balance) || math.IsInf(balance, 0) {
		return 0, errors.New("upstream billing response returned a non-finite balance")
	}
	channel.UpdateBalance(balance)
	return balance, nil
}

func UpdateChannelBalance(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := model.CacheGetChannel(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if channel.Type == constant.ChannelTypeTaskPlugin {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "Task Plugin channels do not support balance queries"})
		return
	}
	if channel.ChannelInfo.IsMultiKey {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "多密钥渠道不支持余额查询",
		})
		return
	}
	result, err := updateChannelBalance(channel)
	if err != nil {
		// Prefix the error with the balance-query mode so the manual refresh
		// endpoint can surface a mode-aware message (and admins can tell a
		// custom-template misconfiguration apart from an upstream outage)
		// instead of the raw expr/transport error text.
		if channel.Type == constant.ChannelTypeNewAPI {
			if bq := channel.GetOtherSettings().BalanceQuery; bq != nil && bq.NormalizedMode() != dto.BalanceQueryModeSubscription {
				err = fmt.Errorf("%s balance query failed: %w", bq.NormalizedMode(), err)
			}
		}
		common.ApiError(c, err)
		return
	}
	if channel.Type == constant.ChannelTypeNewAPI {
		if err := channel.ClearBalanceQueryFailure(); err != nil {
			common.SysError(fmt.Sprintf("failed to clear balance query failure stamp: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	response := gin.H{
		"success": true,
		"message": "",
	}
	if result.RawResponse == "" {
		response["balance"] = result.Balance
	} else {
		response["raw_response"] = result.RawResponse
	}
	c.JSON(http.StatusOK, response)
}

func updateAllChannelsBalance() error {
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return err
	}
	for _, channel := range channels {
		if channel.Status != common.ChannelStatusEnabled {
			continue
		}
		if channel.ChannelInfo.IsMultiKey {
			continue // skip multi-key channels
		}
		// TODO: support Azure
		//if channel.Type != common.ChannelTypeOpenAI && channel.Type != common.ChannelTypeCustom {
		//	continue
		//}
		result, err := updateChannelBalance(channel)
		// New API channels using user_api/custom balance queries are exempt
		// from both branches below: the queried quota may belong to a
		// different account than the channel key, so neither a zero balance
		// nor a query failure describes the relay key's usability.
		balanceQuery := channel.GetOtherSettings().BalanceQuery
		newAPISelfQueriedBalance := channel.Type == constant.ChannelTypeNewAPI &&
			balanceQuery.NormalizedMode() != dto.BalanceQueryModeSubscription
		if err != nil {
			if newAPISelfQueriedBalance {
				common.SysLog(fmt.Sprintf("failed to update New API channel balance: channel_id=%d, mode=%s, error=%v",
					channel.Id, balanceQuery.NormalizedMode(), err))
				// Stamp the failure so the dashboard can tell "balance is
				// really $5" apart from "balance was $5 three days ago and
				// every query since failed": channel_info keeps the last
				// successful value (Balance/BalanceUpdatedTime are untouched)
				// while this timestamp records the most recent failed
				// attempt. The write is best-effort — a failing timestamp
				// update must not mask the query error itself.
				failedAt := time.Now().Unix()
				if saveErr := channel.MarkBalanceQueryFailure(failedAt); saveErr != nil {
					common.SysError(fmt.Sprintf("failed to stamp balance query failure: channel_id=%d, error=%v", channel.Id, saveErr))
				}
			}
			continue
		}
		if channel.Type == constant.ChannelTypeNewAPI {
			// The query succeeded: clear any earlier failure stamp so the
			// dashboard stops flagging the balance as stale, including after
			// switching to subscription mode. Best-effort, like the failure-side write.
			if saveErr := channel.ClearBalanceQueryFailure(); saveErr != nil {
				common.SysError(fmt.Sprintf("failed to clear balance query failure stamp: channel_id=%d, error=%v", channel.Id, saveErr))
			}
		}
		if !newAPISelfQueriedBalance && result.RawResponse == "" {
			// err is nil & balance <= 0 means quota is used up.
			if result.Balance <= 0 {
				service.DisableChannel(*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, "", channel.GetAutoBan()), "余额不足")
			}
		}
		time.Sleep(common.RequestInterval)
	}
	return nil
}

func UpdateAllChannelsBalance(c *gin.Context) {
	// TODO: make it async
	err := updateAllChannelsBalance()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func AutomaticallyUpdateChannels(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Minute)
		common.SysLog("updating all channels")
		_ = updateAllChannelsBalance()
		common.SysLog("channels update done")
	}
}

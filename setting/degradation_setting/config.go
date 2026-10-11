package degradation_setting

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type ProbeConfig struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	SystemPrompt  string   `json:"system_prompt,omitempty"`
	Prompt        string   `json:"prompt"`
	Keywords      []string `json:"keywords"`
	ExcludeWords  []string `json:"exclude_words,omitempty"`
	MatchRule     string   `json:"match_rule"` // "any" | "all" | "regex"
	CaseSensitive bool     `json:"case_sensitive"`
	MaxTokens     int      `json:"max_tokens"`
	Temperature   *float64 `json:"temperature,omitempty"`
}

type ModelProbeBinding struct {
	ModelName       string   `json:"model_name"`
	Enabled         bool     `json:"enabled"`
	ProbeIDs        []string `json:"probe_ids,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"` // "off" | "low" | "medium" | "high" | "custom"
	ThinkingBudget  int      `json:"thinking_budget,omitempty"`
}

type DetectionTarget struct {
	ChannelID   int                 `json:"channel_id"`
	ChannelName string              `json:"channel_name,omitempty"`
	Models      []string            `json:"models,omitempty"`
	Bindings    []ModelProbeBinding `json:"bindings,omitempty"`
}

type DegradationSetting struct {
	Enabled            bool              `json:"enabled"`
	IntervalMinutes    int               `json:"interval_minutes"`
	Concurrency        int               `json:"concurrency"`
	ChannelJitterMs    int               `json:"channel_jitter_ms"`
	TimeoutSeconds     int               `json:"timeout_seconds"`
	RetentionDays      int               `json:"retention_days"`
	PublicMaskTopology bool              `json:"public_mask_topology"`
	Targets            []DetectionTarget `json:"targets"`
	Probes             []ProbeConfig     `json:"probes"`
}

const (
	DefaultIntervalMinutes = 10
	MinIntervalMinutes     = 1
	MaxIntervalMinutes     = 1440

	DefaultConcurrency = 8
	MinConcurrency     = 1
	MaxConcurrency     = 32

	DefaultChannelJitterMs = 300
	MinChannelJitterMs     = 50
	MaxChannelJitterMs     = 5000

	DefaultTimeoutSeconds = 45
	MinTimeoutSeconds     = 5
	MaxTimeoutSeconds     = 180

	DefaultRetentionDays = 7
	MinRetentionDays     = 1
	MaxRetentionDays     = 90

	MinThinkingBudget = 0
	MaxThinkingBudget = 65536

	MinProbeMaxTokens = 1
	MaxProbeMaxTokens = 1024

	MinTemperature = 0
	MaxTemperature = 2

	// 与巡检日志表的 probe_id varchar(64) / probe_name varchar(128) 列宽对齐。
	MaxProbeIdLength   = 64
	MaxProbeNameLength = 128

	// Collection limits keep one option update and one execution round bounded
	// even when an administrator imports a very large JSON document.
	MaxProbes                  = 64
	MaxProbeKeywords           = 64
	MaxProbeExcludeWords       = 32
	MaxProbeKeywordLength      = 256
	MaxProbePromptLength       = 4096
	MaxProbeSystemPromptLength = 4096
	MaxTargets                 = 128
	MaxTargetModels            = 256
	MaxTargetBindings          = 256
	MaxTargetChannelNameLength = 128
	MaxTargetModelNameLength   = 256
	MaxBindingProbeIDs         = 64
	MaxTasksPerRound           = 10000
)

var (
	degradationSetting = DegradationSetting{
		Enabled:            false,
		IntervalMinutes:    DefaultIntervalMinutes,
		Concurrency:        DefaultConcurrency,
		ChannelJitterMs:    DefaultChannelJitterMs,
		TimeoutSeconds:     DefaultTimeoutSeconds,
		RetentionDays:      DefaultRetentionDays,
		PublicMaskTopology: true,
		Targets:            []DetectionTarget{},
		Probes:             []ProbeConfig{},
	}
)

func init() {
	config.GlobalConfig.Register("degradation_setting", &degradationSetting)
}

func GetDegradationSetting() DegradationSetting {
	// Registered configs are written by the option system via reflection while
	// it holds common.OptionMapRWMutex (updateOptionMap -> handleConfigUpdate,
	// also from the periodic SyncOptions loop), so readers must share that
	// lock; a package-local mutex would not cover the reflection write path.
	common.OptionMapRWMutex.RLock()
	copySetting := degradationSetting

	// Targets/Probes are nested slices whose backing arrays are shared with
	// the global after a struct copy. Callers today only read them, but the
	// JSON round-trip enforces value semantics so a future in-place mutation
	// cannot corrupt the global config.
	// Keep the read lock until copying finishes: option writes reuse these arrays.
	if b, err := common.Marshal(copySetting); err == nil {
		var deep DegradationSetting
		if err := common.Unmarshal(b, &deep); err == nil {
			copySetting = deep
		}
	}
	common.OptionMapRWMutex.RUnlock()

	copySetting.IntervalMinutes = normalizeInt(copySetting.IntervalMinutes, DefaultIntervalMinutes, MinIntervalMinutes, MaxIntervalMinutes)
	copySetting.Concurrency = normalizeInt(copySetting.Concurrency, DefaultConcurrency, MinConcurrency, MaxConcurrency)
	copySetting.ChannelJitterMs = normalizeInt(copySetting.ChannelJitterMs, DefaultChannelJitterMs, MinChannelJitterMs, MaxChannelJitterMs)
	copySetting.TimeoutSeconds = normalizeInt(copySetting.TimeoutSeconds, DefaultTimeoutSeconds, MinTimeoutSeconds, MaxTimeoutSeconds)
	copySetting.RetentionDays = normalizeInt(copySetting.RetentionDays, DefaultRetentionDays, MinRetentionDays, MaxRetentionDays)

	return copySetting
}

func ValidateDegradationSetting(s DegradationSetting) error {
	if s.IntervalMinutes < MinIntervalMinutes || s.IntervalMinutes > MaxIntervalMinutes {
		return fmt.Errorf("interval_minutes must be between %d and %d", MinIntervalMinutes, MaxIntervalMinutes)
	}
	if s.Concurrency < MinConcurrency || s.Concurrency > MaxConcurrency {
		return fmt.Errorf("concurrency must be between %d and %d", MinConcurrency, MaxConcurrency)
	}
	if s.ChannelJitterMs < MinChannelJitterMs || s.ChannelJitterMs > MaxChannelJitterMs {
		return fmt.Errorf("channel_jitter_ms must be between %d and %d", MinChannelJitterMs, MaxChannelJitterMs)
	}
	if s.TimeoutSeconds < MinTimeoutSeconds || s.TimeoutSeconds > MaxTimeoutSeconds {
		return fmt.Errorf("timeout_seconds must be between %d and %d", MinTimeoutSeconds, MaxTimeoutSeconds)
	}
	if s.RetentionDays < MinRetentionDays || s.RetentionDays > MaxRetentionDays {
		return fmt.Errorf("retention_days must be between %d and %d", MinRetentionDays, MaxRetentionDays)
	}

	if err := validateProbes(s.Probes); err != nil {
		return err
	}
	return validateTargets(s.Targets)
}

func validateProbes(probes []ProbeConfig) error {
	if len(probes) > MaxProbes {
		return fmt.Errorf("probes cannot contain more than %d entries", MaxProbes)
	}
	probeIDs := make(map[string]bool)
	for _, p := range probes {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return fmt.Errorf("probe id cannot be empty")
		}
		if probeIDs[id] {
			return fmt.Errorf("duplicate probe id: %s", id)
		}
		probeIDs[id] = true

		if utf8.RuneCountInString(p.ID) > MaxProbeIdLength {
			return fmt.Errorf("probe id %s exceeds %d characters", id, MaxProbeIdLength)
		}
		probeName := strings.TrimSpace(p.Name)
		if probeName == "" {
			return fmt.Errorf("probe name cannot be empty for %s", id)
		}
		if utf8.RuneCountInString(p.Name) > MaxProbeNameLength {
			return fmt.Errorf("probe name for %s exceeds %d characters", id, MaxProbeNameLength)
		}
		if strings.TrimSpace(p.Prompt) == "" {
			return fmt.Errorf("probe prompt cannot be empty for %s", id)
		}
		if utf8.RuneCountInString(p.Prompt) > MaxProbePromptLength {
			return fmt.Errorf("probe prompt for %s exceeds %d characters", id, MaxProbePromptLength)
		}
		if utf8.RuneCountInString(p.SystemPrompt) > MaxProbeSystemPromptLength {
			return fmt.Errorf("probe system_prompt for %s exceeds %d characters", id, MaxProbeSystemPromptLength)
		}
		switch p.MatchRule {
		case "", "any", "all", "regex":
		default:
			return fmt.Errorf("invalid match_rule %q for %s (must be any, all, or regex)", p.MatchRule, id)
		}
		// Without keywords the matcher can never pass, so every round would
		// report a false "mismatched" for any channel using the probe.
		hasKeyword := false
		for _, k := range p.Keywords {
			if strings.TrimSpace(k) != "" {
				hasKeyword = true
				break
			}
		}
		if !hasKeyword {
			return fmt.Errorf("probe keywords must contain at least one non-empty keyword for %s", id)
		}
		if len(p.Keywords) > MaxProbeKeywords {
			return fmt.Errorf("probe keywords for %s cannot contain more than %d entries", id, MaxProbeKeywords)
		}
		for _, keyword := range p.Keywords {
			if utf8.RuneCountInString(keyword) > MaxProbeKeywordLength {
				return fmt.Errorf("probe keyword for %s exceeds %d characters", id, MaxProbeKeywordLength)
			}
		}
		if len(p.ExcludeWords) > MaxProbeExcludeWords {
			return fmt.Errorf("probe exclude_words for %s cannot contain more than %d entries", id, MaxProbeExcludeWords)
		}
		for _, excludeWord := range p.ExcludeWords {
			if utf8.RuneCountInString(excludeWord) > MaxProbeKeywordLength {
				return fmt.Errorf("probe exclude word for %s exceeds %d characters", id, MaxProbeKeywordLength)
			}
		}
		if p.MaxTokens < MinProbeMaxTokens || p.MaxTokens > MaxProbeMaxTokens {
			return fmt.Errorf("max_tokens for %s must be between %d and %d", id, MinProbeMaxTokens, MaxProbeMaxTokens)
		}
		if p.Temperature != nil && (*p.Temperature < MinTemperature || *p.Temperature > MaxTemperature) {
			return fmt.Errorf("temperature for %s must be between %d and %d", id, MinTemperature, MaxTemperature)
		}
		// Regex keywords that fail to compile can never match, so every round
		// would report a false "mismatched" for channels using the probe.
		if p.MatchRule == "regex" {
			for _, pattern := range p.Keywords {
				if strings.TrimSpace(pattern) == "" {
					continue
				}
				if _, err := regexp.Compile(pattern); err != nil {
					return fmt.Errorf("invalid regex keyword %q for %s: %w", pattern, id, err)
				}
			}
		}
	}
	return nil
}

func validateTargets(targets []DetectionTarget) error {
	if len(targets) > MaxTargets {
		return fmt.Errorf("targets cannot contain more than %d entries", MaxTargets)
	}
	channelIDs := make(map[int]bool)
	for _, t := range targets {
		if t.ChannelID <= 0 {
			return fmt.Errorf("target channel_id must be a positive integer")
		}
		if channelIDs[t.ChannelID] {
			return fmt.Errorf("duplicate target channel_id: %d", t.ChannelID)
		}
		channelIDs[t.ChannelID] = true
		if utf8.RuneCountInString(t.ChannelName) > MaxTargetChannelNameLength {
			return fmt.Errorf("target channel_name for channel %d exceeds %d characters", t.ChannelID, MaxTargetChannelNameLength)
		}

		if len(t.Models) > MaxTargetModels {
			return fmt.Errorf("target models for channel %d cannot contain more than %d entries", t.ChannelID, MaxTargetModels)
		}
		modelNames := make(map[string]bool, len(t.Models))
		for _, m := range t.Models {
			modelName := strings.TrimSpace(m)
			if modelName == "" {
				return fmt.Errorf("target model name cannot be empty for channel %d", t.ChannelID)
			}
			if utf8.RuneCountInString(m) > MaxTargetModelNameLength {
				return fmt.Errorf("target model name for channel %d exceeds %d characters", t.ChannelID, MaxTargetModelNameLength)
			}
			if modelNames[modelName] {
				return fmt.Errorf("duplicate target model name %s for channel %d", modelName, t.ChannelID)
			}
			modelNames[modelName] = true
		}

		if len(t.Bindings) > MaxTargetBindings {
			return fmt.Errorf("target bindings for channel %d cannot contain more than %d entries", t.ChannelID, MaxTargetBindings)
		}
		bindingModels := make(map[string]bool)
		for _, b := range t.Bindings {
			bindingModelName := strings.TrimSpace(b.ModelName)
			if bindingModelName == "" {
				return fmt.Errorf("binding model_name cannot be empty for channel %d", t.ChannelID)
			}
			if bindingModels[bindingModelName] {
				return fmt.Errorf("duplicate binding model_name %s for channel %d", bindingModelName, t.ChannelID)
			}
			if utf8.RuneCountInString(b.ModelName) > MaxTargetModelNameLength {
				return fmt.Errorf("binding model_name for channel %d exceeds %d characters", t.ChannelID, MaxTargetModelNameLength)
			}
			if len(b.ProbeIDs) > MaxBindingProbeIDs {
				return fmt.Errorf("binding probe_ids for model %s cannot contain more than %d entries", b.ModelName, MaxBindingProbeIDs)
			}
			probeIDs := make(map[string]bool, len(b.ProbeIDs))
			for _, probeID := range b.ProbeIDs {
				if strings.TrimSpace(probeID) == "" {
					return fmt.Errorf("binding probe_id cannot be empty for model %s", b.ModelName)
				}
				probeID = strings.TrimSpace(probeID)
				if probeIDs[probeID] {
					return fmt.Errorf("duplicate binding probe_id %s for model %s", probeID, b.ModelName)
				}
				probeIDs[probeID] = true
			}
			bindingModels[bindingModelName] = true

			switch b.ReasoningEffort {
			case "", "off", "low", "medium", "high", "custom":
			default:
				return fmt.Errorf("invalid reasoning_effort %q for model %s (must be off, low, medium, high, or custom)", b.ReasoningEffort, b.ModelName)
			}
			if b.ThinkingBudget < MinThinkingBudget || b.ThinkingBudget > MaxThinkingBudget {
				return fmt.Errorf("thinking_budget for model %s must be between %d and %d", b.ModelName, MinThinkingBudget, MaxThinkingBudget)
			}
			// A custom binding must carry an actual positive thinking budget.
			if b.ReasoningEffort == "custom" && b.ThinkingBudget <= 0 {
				return fmt.Errorf("thinking_budget must be positive when reasoning_effort is custom for model %s", b.ModelName)
			}
		}
	}
	return nil
}

// ProbesOptionKey is the generic option API key that carries the probe list
// (model/option.go routes it to ValidateProbesJSON before persisting).
const ProbesOptionKey = "degradation_setting.probes"

// TargetsOptionKey is the generic option API key that carries the detection
// target list (model/option.go routes it to ValidateTargetsJSON).
const TargetsOptionKey = "degradation_setting.targets"

// ValidateProbesJSON validates the raw JSON string stored in the
// degradation_setting.probes option before it is persisted.
func ValidateProbesJSON(value string) error {
	var probes []ProbeConfig
	if err := common.Unmarshal([]byte(value), &probes); err != nil {
		return fmt.Errorf("invalid probes json: %w", err)
	}
	return validateProbes(probes)
}

// ValidateTargetsJSON validates the raw JSON string stored in the
// degradation_setting.targets option before it is persisted.
func ValidateTargetsJSON(value string) error {
	var targets []DetectionTarget
	if err := common.Unmarshal([]byte(value), &targets); err != nil {
		return fmt.Errorf("invalid targets json: %w", err)
	}
	return validateTargets(targets)
}

// ValidateSettingOption validates a single degradation_setting.* option value
// before the option system persists it. List keys are validated as standalone
// JSON; scalar keys are range-checked directly against their own bounds — each
// scalar is independently bounded and no cross-field constraint exists, so a
// merged re-validation against the shared setting would only add a concurrency
// window. Out-of-range scalars are still rejected at save time instead of
// being silently clamped at read time.
func ValidateSettingOption(key string, value string) error {
	switch key {
	case ProbesOptionKey:
		return ValidateProbesJSON(value)
	case TargetsOptionKey:
		return ValidateTargetsJSON(value)
	}

	field := strings.TrimPrefix(key, "degradation_setting.")
	switch field {
	case "enabled", "public_mask_topology":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid %s: %w", key, err)
		}
		return nil
	case "interval_minutes", "concurrency", "channel_jitter_ms", "timeout_seconds", "retention_days":
		n, err := strconv.Atoi(value)
		if err != nil {
			// Accept only mathematically integral float-formatted strings, such
			// as "10.000000"; fractional values must not be truncated.
			f, fErr := strconv.ParseFloat(value, 64)
			if fErr != nil || math.IsNaN(f) || math.IsInf(f, 0) || math.Trunc(f) != f || f < -float64(1<<63) || f >= float64(1<<63) {
				return fmt.Errorf("invalid %s: %w", key, err)
			}
			n = int(f)
		}
		var minVal, maxVal int
		switch field {
		case "interval_minutes":
			minVal, maxVal = MinIntervalMinutes, MaxIntervalMinutes
		case "concurrency":
			minVal, maxVal = MinConcurrency, MaxConcurrency
		case "channel_jitter_ms":
			minVal, maxVal = MinChannelJitterMs, MaxChannelJitterMs
		case "timeout_seconds":
			minVal, maxVal = MinTimeoutSeconds, MaxTimeoutSeconds
		case "retention_days":
			minVal, maxVal = MinRetentionDays, MaxRetentionDays
		}
		if n < minVal || n > maxVal {
			return fmt.Errorf("%s must be between %d and %d", key, minVal, maxVal)
		}
		return nil
	default:
		// Unknown keys are ignored by the config reflection; let them through.
		return nil
	}
}

func normalizeInt(val, defVal, minVal, maxVal int) int {
	if val < minVal {
		return defVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
}

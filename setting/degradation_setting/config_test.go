package degradation_setting

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDegradationSettingKeepsSnapshotIndependentOfUpdates(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	oldSetting := degradationSetting
	degradationSetting = DegradationSetting{
		Targets: []DetectionTarget{{ChannelID: 1, Models: []string{"m1"}, Bindings: []ModelProbeBinding{{ModelName: "m1", ProbeIDs: []string{"p1"}}}}},
		Probes:  []ProbeConfig{{ID: "p1", Keywords: []string{"original"}, Temperature: lo.ToPtr(0.0)}},
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		degradationSetting = oldSetting
		common.OptionMapRWMutex.Unlock()
	})
	snapshot := GetDegradationSetting()
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		common.OptionMapRWMutex.Lock()
		err := config.UpdateConfigFromMap(&degradationSetting, map[string]string{
			"targets": `[{"channel_id":1,"models":["m2"],"bindings":[{"model_name":"m2","probe_ids":["p2"]}]}]`,
			"probes":  `[{"id":"p2","keywords":["updated"],"temperature":1}]`,
		})
		common.OptionMapRWMutex.Unlock()
		assert.NoError(t, err)
	}()
	go func() {
		defer wg.Done()
		<-start
		read := GetDegradationSetting()
		assert.Equal(t, read.Probes[0].ID, read.Targets[0].Bindings[0].ProbeIDs[0])
	}()
	close(start)
	wg.Wait()
	assert.Equal(t, "m1", snapshot.Targets[0].Models[0])
	assert.Equal(t, []string{"p1"}, snapshot.Targets[0].Bindings[0].ProbeIDs)
	assert.Equal(t, []string{"original"}, snapshot.Probes[0].Keywords)
	assert.Equal(t, 0.0, *snapshot.Probes[0].Temperature)
	snapshot.Targets[0].Models[0] = "caller-mutated"
	snapshot.Probes[0].Keywords[0] = "caller-mutated"
	current := GetDegradationSetting()
	assert.Equal(t, "m2", current.Targets[0].Models[0])
	assert.Equal(t, []string{"updated"}, current.Probes[0].Keywords)
}

func TestValidateDegradationSetting(t *testing.T) {
	valid := DegradationSetting{
		Enabled:         true,
		IntervalMinutes: 10,
		Concurrency:     8,
		ChannelJitterMs: 300,
		TimeoutSeconds:  30,
		RetentionDays:   7,
		Probes: []ProbeConfig{
			{
				ID:        "probe_1",
				Name:      "Test Probe 1",
				Prompt:    "Hello",
				Keywords:  []string{"Hi"},
				MaxTokens: 256,
			},
		},
	}
	require.NoError(t, ValidateDegradationSetting(valid))

	invalidInterval := valid
	invalidInterval.IntervalMinutes = 0
	assert.Error(t, ValidateDegradationSetting(invalidInterval))

	invalidConcurrency := valid
	invalidConcurrency.Concurrency = 33
	assert.Error(t, ValidateDegradationSetting(invalidConcurrency))

	duplicateProbe := valid
	duplicateProbe.Probes = append(duplicateProbe.Probes, ProbeConfig{
		ID:     "probe_1",
		Name:   "Dup",
		Prompt: "Prompt",
	})
	assert.Error(t, ValidateDegradationSetting(duplicateProbe))
}

func TestValidateProbesJSON(t *testing.T) {
	validJSON := `[{"id":"probe_1","name":"Test Probe","prompt":"Hello","keywords":["Hi"],"max_tokens":256}]`
	require.NoError(t, ValidateProbesJSON(validJSON))

	emptyJSON := `[]`
	require.NoError(t, ValidateProbesJSON(emptyJSON))

	noKeywordsJSON := `[{"id":"probe_1","name":"A","prompt":"Hello","max_tokens":256}]`
	assert.Error(t, ValidateProbesJSON(noKeywordsJSON))

	duplicateJSON := `[
		{"id":"probe_1","name":"A","prompt":"Hello","keywords":["Hi"],"max_tokens":256},
		{"id":"probe_1","name":"B","prompt":"Hello","keywords":["Hi"],"max_tokens":256}
	]`
	assert.Error(t, ValidateProbesJSON(duplicateJSON))

	emptyIDJSON := `[{"id":"  ","name":"A","prompt":"Hello"}]`
	assert.Error(t, ValidateProbesJSON(emptyIDJSON))

	emptyPromptJSON := `[{"id":"probe_1","name":"A","prompt":"  "}]`
	assert.Error(t, ValidateProbesJSON(emptyPromptJSON))

	assert.Error(t, ValidateProbesJSON(`not-json`))
	assert.Error(t, ValidateProbesJSON(`{"id":"probe_1"}`))
}

func TestValidateProbesNumericBounds(t *testing.T) {
	// Fresh backing array per case: slice elements would otherwise share
	// state between cases.
	newBase := func() DegradationSetting {
		return DegradationSetting{
			IntervalMinutes: 10,
			Concurrency:     8,
			ChannelJitterMs: 300,
			TimeoutSeconds:  30,
			RetentionDays:   7,
			Probes: []ProbeConfig{
				{ID: "probe_1", Name: "P", Prompt: "Hello", Keywords: []string{"Hi"}, MaxTokens: 256},
			},
		}
	}

	zeroTokens := newBase()
	zeroTokens.Probes[0].MaxTokens = 0
	assert.Error(t, ValidateDegradationSetting(zeroTokens))

	oversizedTokens := newBase()
	oversizedTokens.Probes[0].MaxTokens = MaxProbeMaxTokens + 1
	assert.Error(t, ValidateDegradationSetting(oversizedTokens))

	lowTemp := newBase()
	lowTemp.Probes[0].Temperature = lo.ToPtr(-0.1)
	assert.Error(t, ValidateDegradationSetting(lowTemp))

	highTemp := newBase()
	highTemp.Probes[0].Temperature = lo.ToPtr(2.1)
	assert.Error(t, ValidateDegradationSetting(highTemp))

	validTemp := newBase()
	validTemp.Probes[0].Temperature = lo.ToPtr(1.5)
	require.NoError(t, ValidateDegradationSetting(validTemp))
}

func TestValidateProbesMatchRule(t *testing.T) {
	base := DegradationSetting{
		IntervalMinutes: 10,
		Concurrency:     8,
		ChannelJitterMs: 300,
		TimeoutSeconds:  30,
		RetentionDays:   7,
		Probes: []ProbeConfig{
			{ID: "probe_1", Name: "P", Prompt: "Hello", Keywords: []string{"Hi"}, MaxTokens: 256},
		},
	}
	// empty match_rule keeps the legacy "any" behavior
	require.NoError(t, ValidateDegradationSetting(base))

	anyRule := base
	anyRule.Probes[0].MatchRule = "any"
	require.NoError(t, ValidateDegradationSetting(anyRule))

	regexRule := base
	regexRule.Probes[0].MatchRule = "regex"
	require.NoError(t, ValidateDegradationSetting(regexRule))

	invalidRule := base
	invalidRule.Probes[0].MatchRule = "startswith"
	assert.Error(t, ValidateDegradationSetting(invalidRule))

	invalidRegex := base
	invalidRegex.Probes[0].MatchRule = "regex"
	invalidRegex.Probes[0].Keywords = []string{"result ([579"}
	assert.Error(t, ValidateDegradationSetting(invalidRegex))

	validRegex := base
	validRegex.Probes[0].MatchRule = "regex"
	validRegex.Probes[0].Keywords = []string{"result .*579"}
	require.NoError(t, ValidateDegradationSetting(validRegex))

	// Empty keywords are skipped by the matcher and by this validation.
	blankKeywordRegex := base
	blankKeywordRegex.Probes[0].MatchRule = "regex"
	blankKeywordRegex.Probes[0].Keywords = []string{" ", "result .*579"}
	require.NoError(t, ValidateDegradationSetting(blankKeywordRegex))
}

func TestValidateProbesFieldLength(t *testing.T) {
	// probe id/name 列宽上限与巡检日志表的 varchar(64)/varchar(128) 对齐。
	probeJSON := func(id, name string) string {
		return fmt.Sprintf(`[{"id":%q,"name":%q,"prompt":"Hello","keywords":["Hi"],"max_tokens":256}]`, id, name)
	}

	require.NoError(t, ValidateProbesJSON(probeJSON(
		strings.Repeat("a", MaxProbeIdLength),
		strings.Repeat("N", MaxProbeNameLength),
	)))
	assert.Error(t, ValidateProbesJSON(probeJSON(
		strings.Repeat("a", MaxProbeIdLength+1),
		"N",
	)))
	assert.Error(t, ValidateProbesJSON(probeJSON(
		"a",
		strings.Repeat("N", MaxProbeNameLength+1),
	)))
}

func TestValidateProbesRawFieldLength(t *testing.T) {
	for _, tc := range []struct {
		name    string
		id      string
		label   string
		wantErr string
	}{
		{"ID at limit with whitespace and Unicode", " " + strings.Repeat("中", MaxProbeIdLength-1), "Probe", ""},
		{"ID over limit with leading whitespace", strings.Repeat(" ", MaxProbeIdLength) + "p1", "Probe", "probe id"},
		{"ID over limit with trailing whitespace", strings.Repeat("中", MaxProbeIdLength) + " ", "Probe", "probe id"},
		{"name at limit with whitespace and Unicode", "p1", " " + strings.Repeat("名", MaxProbeNameLength-1), ""},
		{"name over limit with leading whitespace", "p1", " " + strings.Repeat("名", MaxProbeNameLength), "probe name"},
		{"name over limit with trailing whitespace", "p1", strings.Repeat("名", MaxProbeNameLength) + " ", "probe name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, err := common.Marshal([]ProbeConfig{{
				ID: tc.id, Name: tc.label, Prompt: "Reply ok", Keywords: []string{"ok"}, MaxTokens: 16,
			}})
			require.NoError(t, err)
			err = ValidateSettingOption(ProbesOptionKey, string(value))
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestValidateDegradationSettingCollectionBounds(t *testing.T) {
	baseProbe := ProbeConfig{
		ID:        "probe",
		Name:      "Probe",
		Prompt:    "Hello",
		Keywords:  []string{"Hi"},
		MaxTokens: 256,
	}

	tooManyProbes := make([]ProbeConfig, MaxProbes+1)
	for i := range tooManyProbes {
		tooManyProbes[i] = baseProbe
		tooManyProbes[i].ID = fmt.Sprintf("probe-%d", i)
	}
	assert.Error(t, ValidateDegradationSetting(DegradationSetting{
		IntervalMinutes: 10, Concurrency: 8, ChannelJitterMs: 300, TimeoutSeconds: 30, RetentionDays: 7,
		Probes: tooManyProbes,
	}))

	longPrompt := baseProbe
	longPrompt.Prompt = strings.Repeat("x", MaxProbePromptLength+1)
	assert.Error(t, ValidateDegradationSetting(DegradationSetting{
		IntervalMinutes: 10, Concurrency: 8, ChannelJitterMs: 300, TimeoutSeconds: 30, RetentionDays: 7,
		Probes: []ProbeConfig{longPrompt},
	}))

	tooManyModels := make([]string, MaxTargetModels+1)
	for i := range tooManyModels {
		tooManyModels[i] = fmt.Sprintf("model-%d", i)
	}
	assert.Error(t, ValidateDegradationSetting(DegradationSetting{
		IntervalMinutes: 10, Concurrency: 8, ChannelJitterMs: 300, TimeoutSeconds: 30, RetentionDays: 7,
		Targets: []DetectionTarget{{ChannelID: 1, Models: tooManyModels}},
	}))

	tooManyBindingProbeIDs := ModelProbeBinding{
		ModelName: "model",
		ProbeIDs:  make([]string, MaxBindingProbeIDs+1),
	}
	assert.Error(t, ValidateDegradationSetting(DegradationSetting{
		IntervalMinutes: 10, Concurrency: 8, ChannelJitterMs: 300, TimeoutSeconds: 30, RetentionDays: 7,
		Targets: []DetectionTarget{{ChannelID: 1, Bindings: []ModelProbeBinding{tooManyBindingProbeIDs}}},
	}))
}

func TestValidateTargetsJSON(t *testing.T) {
	validJSON := `[{"channel_id":1,"models":["gpt-4o"],"bindings":[{"model_name":"gpt-4o","enabled":true,"reasoning_effort":"high","thinking_budget":2048}]}]`
	require.NoError(t, ValidateTargetsJSON(validJSON))

	emptyJSON := `[]`
	require.NoError(t, ValidateTargetsJSON(emptyJSON))

	assert.Error(t, ValidateTargetsJSON(`not-json`))
	assert.Error(t, ValidateTargetsJSON(`[{"channel_id":0}]`))
	assert.Error(t, ValidateTargetsJSON(`[{"channel_id":1,"models":["  "]}]`))

	duplicateChannelJSON := `[{"channel_id":1},{"channel_id":1}]`
	assert.Error(t, ValidateTargetsJSON(duplicateChannelJSON))

	duplicateModelJSON := `[{"channel_id":1,"bindings":[{"model_name":"m"},{"model_name":"m"}]}]`
	assert.Error(t, ValidateTargetsJSON(duplicateModelJSON))

	duplicateTargetModelJSON := `[{"channel_id":1,"models":["m","m"]}]`
	assert.Error(t, ValidateTargetsJSON(duplicateTargetModelJSON))

	duplicateProbeIDJSON := `[{"channel_id":1,"bindings":[{"model_name":"m","probe_ids":["p1","p1"]}]}]`
	assert.Error(t, ValidateTargetsJSON(duplicateProbeIDJSON))

	invalidEffortJSON := `[{"channel_id":1,"bindings":[{"model_name":"m","reasoning_effort":"hight"}]}]`
	assert.Error(t, ValidateTargetsJSON(invalidEffortJSON))

	customZeroBudgetJSON := `[{"channel_id":1,"bindings":[{"model_name":"m","reasoning_effort":"custom"}]}]`
	assert.Error(t, ValidateTargetsJSON(customZeroBudgetJSON))

	oversizedBudgetJSON := `[{"channel_id":1,"bindings":[{"model_name":"m","reasoning_effort":"custom","thinking_budget":1000000}]}]`
	assert.Error(t, ValidateTargetsJSON(oversizedBudgetJSON))
}

func TestValidateSettingOptionResourceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		limit int
		apply func(*DegradationSetting, int)
	}{
		{"system prompt", ProbesOptionKey, MaxProbeSystemPromptLength, func(s *DegradationSetting, n int) {
			s.Probes[0].SystemPrompt = strings.Repeat("中", n)
		}},
		{"keywords", ProbesOptionKey, MaxProbeKeywords, func(s *DegradationSetting, n int) {
			s.Probes[0].Keywords = make([]string, n)
			s.Probes[0].Keywords[0] = "ok"
		}},
		{"keyword length", ProbesOptionKey, MaxProbeKeywordLength, func(s *DegradationSetting, n int) {
			s.Probes[0].Keywords = []string{strings.Repeat("中", n)}
		}},
		{"exclude words", ProbesOptionKey, MaxProbeExcludeWords, func(s *DegradationSetting, n int) {
			s.Probes[0].ExcludeWords = make([]string, n)
		}},
		{"exclude word length", ProbesOptionKey, MaxProbeKeywordLength, func(s *DegradationSetting, n int) {
			s.Probes[0].ExcludeWords = []string{strings.Repeat("中", n)}
		}},
		{"targets", TargetsOptionKey, MaxTargets, func(s *DegradationSetting, n int) {
			s.Targets = make([]DetectionTarget, n)
			for i := range s.Targets {
				s.Targets[i].ChannelID = i + 1
			}
		}},
		{"model name", TargetsOptionKey, MaxTargetModelNameLength, func(s *DegradationSetting, n int) {
			s.Targets[0].Models = []string{strings.Repeat("中", n)}
		}},
		{"bindings", TargetsOptionKey, MaxTargetBindings, func(s *DegradationSetting, n int) {
			s.Targets[0].Bindings = make([]ModelProbeBinding, n)
			for i := range s.Targets[0].Bindings {
				s.Targets[0].Bindings[i].ModelName = fmt.Sprintf("model-%d", i)
			}
		}},
		{"binding model name", TargetsOptionKey, MaxTargetModelNameLength, func(s *DegradationSetting, n int) {
			s.Targets[0].Bindings = []ModelProbeBinding{{ModelName: strings.Repeat("中", n)}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range []int{tc.limit, tc.limit + 1} {
				setting := DegradationSetting{
					Probes:  []ProbeConfig{{ID: "p1", Name: "Probe", Prompt: "Reply ok", Keywords: []string{"ok"}, MaxTokens: 16}},
					Targets: []DetectionTarget{{ChannelID: 1}},
				}
				tc.apply(&setting, n)
				var value any = setting.Probes
				if tc.key == TargetsOptionKey {
					value = setting.Targets
				}
				data, err := common.Marshal(value)
				require.NoError(t, err)
				err = ValidateSettingOption(tc.key, string(data))
				if n == tc.limit {
					require.NoError(t, err, "maximum supported size must remain usable")
				} else {
					require.Error(t, err, "oversized option must be rejected before saving")
				}
			}
		})
	}
}

func TestValidateSettingOption(t *testing.T) {
	// List keys delegate to the standalone JSON validators.
	assert.NoError(t, ValidateSettingOption("degradation_setting.probes", `[]`))
	assert.Error(t, ValidateSettingOption("degradation_setting.probes", `not-json`))
	assert.NoError(t, ValidateSettingOption("degradation_setting.targets", `[]`))
	assert.Error(t, ValidateSettingOption("degradation_setting.targets", `[{"channel_id":0}]`))

	// Scalar keys are range-checked directly, so out-of-range values are
	// rejected at save time instead of silently clamped on read.
	assert.NoError(t, ValidateSettingOption("degradation_setting.interval_minutes", "30"))
	assert.Error(t, ValidateSettingOption("degradation_setting.interval_minutes", "0"))
	assert.Error(t, ValidateSettingOption("degradation_setting.interval_minutes", "1441"))
	assert.Error(t, ValidateSettingOption("degradation_setting.concurrency", "33"))
	assert.NoError(t, ValidateSettingOption("degradation_setting.channel_jitter_ms", "300"))
	assert.Error(t, ValidateSettingOption("degradation_setting.channel_jitter_ms", "10"))
	assert.NoError(t, ValidateSettingOption("degradation_setting.timeout_seconds", "45"))
	assert.Error(t, ValidateSettingOption("degradation_setting.timeout_seconds", "1"))
	assert.NoError(t, ValidateSettingOption("degradation_setting.retention_days", "7"))
	assert.Error(t, ValidateSettingOption("degradation_setting.retention_days", "91"))

	// Float-formatted scalars are accepted, mirroring updateConfigFromMap.
	assert.NoError(t, ValidateSettingOption("degradation_setting.interval_minutes", "10.000000"))
	assert.Error(t, ValidateSettingOption("degradation_setting.interval_minutes", "10.9"))
	assert.Error(t, ValidateSettingOption("degradation_setting.interval_minutes", "abc"))

	// Bool keys must parse.
	assert.NoError(t, ValidateSettingOption("degradation_setting.enabled", "true"))
	assert.NoError(t, ValidateSettingOption("degradation_setting.public_mask_topology", "false"))
	assert.Error(t, ValidateSettingOption("degradation_setting.enabled", "yes"))

	// Unknown keys are ignored by the config reflection and pass through.
	assert.NoError(t, ValidateSettingOption("degradation_setting.unknown", "whatever"))
}

func TestValidateDegradationSettingBindings(t *testing.T) {
	base := DegradationSetting{
		IntervalMinutes: 10,
		Concurrency:     8,
		ChannelJitterMs: 300,
		TimeoutSeconds:  30,
		RetentionDays:   7,
	}

	invalidEffort := base
	invalidEffort.Targets = []DetectionTarget{
		{ChannelID: 1, Bindings: []ModelProbeBinding{{ModelName: "m", ReasoningEffort: "ultra"}}},
	}
	assert.Error(t, ValidateDegradationSetting(invalidEffort))

	oversizedBudget := base
	oversizedBudget.Targets = []DetectionTarget{
		{ChannelID: 1, Bindings: []ModelProbeBinding{{ModelName: "m", ReasoningEffort: "custom", ThinkingBudget: MaxThinkingBudget + 1}}},
	}
	assert.Error(t, ValidateDegradationSetting(oversizedBudget))

	emptyModelName := base
	emptyModelName.Targets = []DetectionTarget{
		{ChannelID: 1, Bindings: []ModelProbeBinding{{ModelName: " "}}},
	}
	assert.Error(t, ValidateDegradationSetting(emptyModelName))
}

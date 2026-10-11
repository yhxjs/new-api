package config_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/degradation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func degradationConfigUpdater(t *testing.T, cfg *degradation_setting.DegradationSetting, fromDB bool) func(map[string]string) error {
	t.Helper()
	if !fromDB {
		return func(options map[string]string) error {
			return config.UpdateConfigFromMap(cfg, options)
		}
	}
	manager := config.NewConfigManager()
	manager.Register("degradation_setting", cfg)
	return func(options map[string]string) error {
		prefixed := make(map[string]string, len(options))
		for key, value := range options {
			prefixed["degradation_setting."+key] = value
		}
		return manager.LoadFromDB(prefixed)
	}
}

func TestDegradationConfigSlicesReplaceOmittedFields(t *testing.T) {
	for _, fromDB := range []bool{false, true} {
		name := "UpdateConfigFromMap"
		if fromDB {
			name = "LoadFromDB"
		}
		t.Run(name, func(t *testing.T) {
			var cfg degradation_setting.DegradationSetting
			update := degradationConfigUpdater(t, &cfg, fromDB)
			require.NoError(t, update(map[string]string{
				"probes":  `[{"id":"probe","system_prompt":"old prompt","exclude_words":["old exclusion"],"temperature":0.8}]`,
				"targets": `[{"channel_id":1,"models":["old-model"],"bindings":[{"model_name":"old-model","enabled":false,"probe_ids":["probe"],"reasoning_effort":"high","thinking_budget":1024}]},{"channel_id":2,"models":["other-model"]}]`,
			}))
			require.NoError(t, update(map[string]string{
				"probes":  `[{"id":"probe","name":"Updated Probe"}]`,
				"targets": `[{"channel_id":2},{"channel_id":3}]`,
			}))

			assert.Equal(t, []degradation_setting.ProbeConfig{{ID: "probe", Name: "Updated Probe"}}, cfg.Probes)
			assert.Equal(t, []degradation_setting.DetectionTarget{{ChannelID: 2}, {ChannelID: 3}}, cfg.Targets)
			require.NoError(t, update(map[string]string{"probes": `[]`, "targets": `[]`}))
			assert.Empty(t, cfg.Probes)
			assert.Empty(t, cfg.Targets)
		})
	}
}

func TestDegradationConfigInvalidSliceUpdateKeepsPreviousValues(t *testing.T) {
	for _, fromDB := range []bool{false, true} {
		name := "UpdateConfigFromMap"
		if fromDB {
			name = "LoadFromDB"
		}
		t.Run(name, func(t *testing.T) {
			var cfg degradation_setting.DegradationSetting
			update := degradationConfigUpdater(t, &cfg, fromDB)
			require.NoError(t, update(map[string]string{
				"probes":  `[{"id":"probe","temperature":0.8,"max_tokens":10}]`,
				"targets": `[{"channel_id":1,"models":["old-model"]}]`,
			}))
			before, err := common.Marshal(cfg)
			require.NoError(t, err)

			require.NoError(t, update(map[string]string{
				"probes":  `[{"id":"changed","temperature":1.2,"max_tokens":"invalid"}]`,
				"targets": `[{"channel_id":3},{"channel_id":"invalid"}]`,
			}))
			after, err := common.Marshal(cfg)
			require.NoError(t, err)
			assert.JSONEq(t, string(before), string(after))
		})
	}
}

func TestUpdateConfigFromMapRejectsFractionalIntegers(t *testing.T) {
	for _, fromDB := range []bool{false, true} {
		name := "UpdateConfigFromMap"
		if fromDB {
			name = "LoadFromDB"
		}
		t.Run(name, func(t *testing.T) {
			cfg := degradation_setting.DegradationSetting{IntervalMinutes: 12}
			update := degradationConfigUpdater(t, &cfg, fromDB)
			require.NoError(t, update(map[string]string{"interval_minutes": "10.9"}))
			assert.Equal(t, 12, cfg.IntervalMinutes)
			require.NoError(t, update(map[string]string{"interval_minutes": "10.000000"}))
			assert.Equal(t, 10, cfg.IntervalMinutes)
		})
	}
}

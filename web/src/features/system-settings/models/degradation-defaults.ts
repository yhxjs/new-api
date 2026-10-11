/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/**
 * degradation_setting.* option defaults shared by the models settings page
 * and the model-mutate drawer's fallback lookup, so the two cannot drift.
 */
export const DEGRADATION_SETTING_DEFAULTS = {
  'degradation_setting.enabled': false,
  'degradation_setting.interval_minutes': 10,
  'degradation_setting.concurrency': 8,
  'degradation_setting.channel_jitter_ms': 300,
  'degradation_setting.timeout_seconds': 45,
  'degradation_setting.retention_days': 7,
  'degradation_setting.public_mask_topology': true,
  'degradation_setting.targets': '[]',
  'degradation_setting.probes': '[]',
} as const

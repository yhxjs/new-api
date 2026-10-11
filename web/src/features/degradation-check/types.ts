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
export type DegradationOverviewStats = {
  pass_rate: number
  total_executions: number
  mismatch_count: number
  error_count: number
  active_targets: number
  next_run_seconds: number
  enabled: boolean
  aggregation_error: boolean
}

export type DegradationProbeTimeline = {
  probe_id: string
  probe_name: string
  pass_rate: number
  slots: number[] // 24 slots: 1=passed, 2=mismatched, 3=error, 4=timeout, 0=no_data
}

export type DegradationSnapshotItem = {
  time: string
  status: number
  probe_name: string
  latency_ms: number
  token_count: number
  reasoning_tokens?: number
  reasoning_effort?: string
  detail?: string
}

export type DegradationModelItem = {
  model_name: string
  enabled: boolean
  reasoning_effort?: string
  pass_rate: number
  total_executions?: number
  avg_latency_ms: number
  probes: DegradationProbeTimeline[]
  latest_snapshot?: DegradationSnapshotItem
}

export type DegradationChannelGroup = {
  channel_id?: number
  channel_name: string
  models: DegradationModelItem[]
}

export type DegradationOverviewData = {
  stats: DegradationOverviewStats
  channels: DegradationChannelGroup[]
}

export type DegradationOverviewResponse = {
  success: boolean
  message: string
  data: DegradationOverviewData
}

export type DegradationCheckLog = {
  id: number
  created_at: string
  channel_id: number
  channel_name: string
  model_name: string
  probe_id: string
  probe_name: string
  status: number
  latency_ms: number
  token_count: number
  reasoning_tokens: number
  detail: string
}

export type DegradationLogsResponse = {
  success: boolean
  message: string
  data: {
    items: DegradationCheckLog[]
    total: number
  }
}

export type DegradationSortOption =
  | 'worst-pass-rate'
  | 'highest-latency'
  | 'most-models'
  | 'alphabetical'

export type DegradationStatusFilter = 'all' | 'normal' | 'degraded' | 'error'

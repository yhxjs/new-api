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
export type ModelStatusBucket = {
  ts: number
  has_data: boolean
  request_count: number
  success_rate: number
}

export type ModelStatusModel = {
  model_name: string
  request_count: number
  success_count: number
  failure_count: number
  success_rate: number
  avg_ttft_ms: number | null
  avg_tps: number | null
  buckets: ModelStatusBucket[]
}

export type ModelStatusSummary = {
  model_count: number
  request_count: number
  success_count: number
  success_rate: number
  attention_count: number
  critical_count: number
}

export type ModelStatusSnapshot = {
  updated_at: number
  window_hours: number
  collection_enabled: boolean
  selected_group: string
  groups: string[]
  summary: ModelStatusSummary
  models: ModelStatusModel[]
}

export type ModelStatusResponse = {
  success: boolean
  message?: string
  data: ModelStatusSnapshot
}

export type ModelStatusSort =
  | 'request-count'
  | 'success-rate'
  | 'first-token-latency'

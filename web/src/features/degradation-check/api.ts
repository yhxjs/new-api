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
import { t } from 'i18next'

import { api } from '@/lib/api'

import type {
  DegradationLogsResponse,
  DegradationOverviewResponse,
} from './types'

export async function getDegradationOverview(): Promise<DegradationOverviewResponse> {
  const res = await api.get<DegradationOverviewResponse>(
    '/api/degradation/overview'
  )
  if (!res.data.success) {
    throw new Error(res.data.message || t('Failed to load degradation data.'))
  }
  return res.data
}

export async function getDegradationLogs(params: {
  channel_id?: number
  model_name?: string
  status?: number
  page?: number
  page_size?: number
}): Promise<DegradationLogsResponse> {
  const res = await api.get<DegradationLogsResponse>('/api/degradation/logs', {
    params,
  })
  if (!res.data.success) {
    throw new Error(res.data.message || t('Failed to load logs'))
  }
  return res.data
}

export async function triggerDegradationRun(): Promise<{
  success: boolean
  message: string
}> {
  const res = await api.post('/api/degradation/run')
  return res.data
}

export async function triggerDegradationTargetRun(
  channelId: number,
  modelName: string
): Promise<{
  success: boolean
  message: string
}> {
  const res = await api.post('/api/degradation/run-target', {
    channel_id: channelId,
    model_name: modelName,
  })
  return res.data
}

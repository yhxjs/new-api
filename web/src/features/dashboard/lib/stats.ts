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
import type { QuotaDataItem } from '@/features/dashboard/types'

/**
 * Safe division: handles NaN and Infinity cases
 */
export function safeDivide(
  value: number,
  divisor: number,
  precision: number = 3
): number {
  const result = value / divisor
  if (Number.isNaN(result) || !Number.isFinite(result)) return 0
  const factor = Math.pow(10, precision)
  return Math.round(result * factor) / factor
}

/**
 * Calculate cache hit rate as percentage (0-100).
 * cache hit rate = (cacheTokens / promptTokens) * 100.
 * Returns 0 if promptTokens <= 0 or result is invalid.
 */
export function calculateCacheHitRate(
  cacheTokens: number,
  promptTokens: number,
  precision: number = 1
): number {
  if (!promptTokens || promptTokens <= 0 || !cacheTokens || cacheTokens <= 0) {
    return 0
  }
  const rate = (cacheTokens / promptTokens) * 100
  return safeDivide(rate, 1, precision)
}

/**
 * Calculate aggregated statistics from quota data
 */
export function calculateDashboardStats(data: QuotaDataItem[]) {
  return data.reduce(
    (acc, item) => ({
      totalQuota: acc.totalQuota + (Number(item.quota) || 0),
      totalCount: acc.totalCount + (Number(item.count) || 0),
      totalTokens: acc.totalTokens + (Number(item.token_used) || 0),
      totalPromptTokens:
        acc.totalPromptTokens + (Number(item.prompt_tokens) || 0),
      totalCompletionTokens:
        acc.totalCompletionTokens + (Number(item.completion_tokens) || 0),
      totalCacheTokens: acc.totalCacheTokens + (Number(item.cache_tokens) || 0),
    }),
    {
      totalQuota: 0,
      totalCount: 0,
      totalTokens: 0,
      totalPromptTokens: 0,
      totalCompletionTokens: 0,
      totalCacheTokens: 0,
    }
  )
}

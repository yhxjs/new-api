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
import { describe, expect, it } from 'vitest'

import {
  calculateCacheHitRate,
  calculateDashboardStats,
  safeDivide,
} from '../lib/stats'
import type { QuotaDataItem } from '../types'

describe('calculateCacheHitRate', () => {
  it('calculates cache hit rate percentage correctly', () => {
    expect(calculateCacheHitRate(452, 1000)).toBe(45.2)
    expect(calculateCacheHitRate(500, 1000)).toBe(50)
    expect(calculateCacheHitRate(1000, 1000)).toBe(100)
  })

  it('handles zero or negative prompt tokens gracefully', () => {
    expect(calculateCacheHitRate(100, 0)).toBe(0)
    expect(calculateCacheHitRate(100, -50)).toBe(0)
  })

  it('handles zero or negative cache tokens gracefully', () => {
    expect(calculateCacheHitRate(0, 1000)).toBe(0)
    expect(calculateCacheHitRate(-10, 1000)).toBe(0)
  })
})

describe('calculateDashboardStats', () => {
  it('aggregates quota, counts, tokens, and prompt/completion/cache tokens', () => {
    const data: QuotaDataItem[] = [
      {
        created_at: 1000,
        quota: 100,
        count: 5,
        token_used: 1200,
        prompt_tokens: 1000,
        completion_tokens: 200,
        cache_tokens: 400,
      },
      {
        created_at: 2000,
        quota: 50,
        count: 2,
        token_used: 800,
        prompt_tokens: 600,
        completion_tokens: 200,
        cache_tokens: 150,
      },
    ]

    const stats = calculateDashboardStats(data)
    expect(stats.totalQuota).toBe(150)
    expect(stats.totalCount).toBe(7)
    expect(stats.totalTokens).toBe(2000)
    expect(stats.totalPromptTokens).toBe(1600)
    expect(stats.totalCompletionTokens).toBe(400)
    expect(stats.totalCacheTokens).toBe(550)
  })

  it('handles empty or missing optional fields without NaN', () => {
    const data: QuotaDataItem[] = [
      {
        created_at: 1000,
      },
    ]

    const stats = calculateDashboardStats(data)
    expect(stats.totalQuota).toBe(0)
    expect(stats.totalCount).toBe(0)
    expect(stats.totalTokens).toBe(0)
    expect(stats.totalPromptTokens).toBe(0)
    expect(stats.totalCompletionTokens).toBe(0)
    expect(stats.totalCacheTokens).toBe(0)
  })
})

describe('safeDivide', () => {
  it('safely divides without NaN or Infinity', () => {
    expect(safeDivide(10, 2)).toBe(5)
    expect(safeDivide(10, 0)).toBe(0)
  })
})

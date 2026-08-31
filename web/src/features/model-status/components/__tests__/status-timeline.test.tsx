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
import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import type { ModelStatusBucket } from '../../types'
import { StatusTimeline } from '../status-timeline'

function createBuckets(): ModelStatusBucket[] {
  const start = 1_788_048_000
  return Array.from({ length: 24 }, (_, index) => ({
    ts: start + index * 3600,
    has_data: true,
    request_count: 5,
    success_rate: 95,
  }))
}

describe('model status timeline', () => {
  test('renders exactly 24 keyboard-focusable hourly cells', () => {
    render(<StatusTimeline buckets={createBuckets()} />)

    expect(screen.getAllByRole('button')).toHaveLength(24)
  })

  test('treats missing data as unknown instead of critical', () => {
    const buckets = createBuckets()
    buckets[0] = {
      ...buckets[0],
      has_data: false,
      request_count: 0,
      success_rate: 0,
    }

    render(<StatusTimeline buckets={buckets} />)

    const unknown = screen.getByRole('button', { name: /No data/ })
    expect(unknown).toHaveAttribute('data-status', 'unknown')
  })

  test('uses warning and healthy states at the exact thresholds', () => {
    const buckets = createBuckets()
    buckets[0] = { ...buckets[0], success_rate: 70 }
    buckets[1] = { ...buckets[1], success_rate: 90 }

    render(<StatusTimeline buckets={buckets} />)

    expect(screen.getByRole('button', { name: /70\.00%/ })).toHaveAttribute(
      'data-status',
      'warning'
    )
    expect(screen.getByRole('button', { name: /90\.00%/ })).toHaveAttribute(
      'data-status',
      'healthy'
    )
  })
})

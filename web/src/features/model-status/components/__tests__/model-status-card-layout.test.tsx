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
import { render, screen, within } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import type { ModelStatusModel } from '../../types'
import { ModelStatusCard } from '../model-status-card'
import { StatusSummary } from '../status-summary'

const MODEL = {
  model_name: 'provider/very-long-model-name-with-context-20260831-preview',
  request_count: 1,
  success_count: 1,
  failure_count: 0,
  success_rate: 100,
  avg_ttft_ms: null,
  avg_tps: null,
  buckets: [],
} satisfies ModelStatusModel

describe('model status card layout', () => {
  test('wraps long model names instead of clipping them', () => {
    render(<ModelStatusCard model={MODEL} />)

    const heading = screen.getByRole('heading', { name: MODEL.model_name })
    expect(heading).toHaveClass('break-all')
    expect(heading).not.toHaveClass('truncate')
  })

  test('allows metric labels to wrap for expanding translations', () => {
    render(<ModelStatusCard model={MODEL} />)

    const card = screen.getByRole('article', { name: MODEL.model_name })
    const labels = [
      'Success rate',
      'Requests',
      'Successful',
      'Failed',
      'Average first token',
      'Average output speed',
    ]

    labels.forEach((label) => {
      const element = within(card).getByText(label)
      expect(element).toHaveClass('text-pretty')
      expect(element).not.toHaveClass('truncate')
    })
  })

  test('allows summary labels and details to wrap for expanding translations', () => {
    render(
      <StatusSummary
        summary={{
          model_count: 1,
          request_count: 1,
          success_count: 1,
          success_rate: 100,
          attention_count: 0,
          critical_count: 0,
        }}
      />
    )

    const summary = screen.getByRole('region', { name: 'Status summary' })
    const labels = [
      'Active models',
      'With requests in this window',
      'Overall success rate',
      'Weighted by request volume',
    ]

    labels.forEach((label) => {
      const element = within(summary).getByText(label)
      expect(element).toHaveClass('text-pretty')
      expect(element).not.toHaveClass('truncate')
    })
  })
})

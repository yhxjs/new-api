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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { ModelStatusPage } from '../index'
import type {
  ModelStatusBucket,
  ModelStatusResponse,
  ModelStatusSnapshot,
} from '../types'

const mocks = vi.hoisted(() => ({
  getModelStatus: vi.fn(),
}))

vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: React.ReactNode }) => props.children,
}))

vi.mock('@/components/page-transition', () => ({
  PageTransition: (props: { children: React.ReactNode }) => props.children,
}))

vi.mock('../api', () => ({
  getModelStatus: mocks.getModelStatus,
}))

function createBuckets(successRate: number): ModelStatusBucket[] {
  const start = 1_788_048_000
  return Array.from({ length: 24 }, (_, index) => ({
    ts: start + index * 3600,
    has_data: true,
    request_count: 5,
    success_rate: successRate,
  }))
}

function createSnapshot(
  overrides: Partial<ModelStatusSnapshot> = {}
): ModelStatusSnapshot {
  return {
    updated_at: 1_788_134_400,
    window_hours: 24,
    collection_enabled: true,
    selected_group: 'all',
    groups: ['default', 'vip'],
    summary: {
      model_count: 2,
      request_count: 110,
      success_count: 106,
      success_rate: 96.36,
      attention_count: 1,
      critical_count: 0,
    },
    models: [
      {
        model_name: 'alpha-large',
        request_count: 100,
        success_count: 99,
        failure_count: 1,
        success_rate: 99,
        avg_ttft_ms: null,
        avg_tps: null,
        buckets: createBuckets(99),
      },
      {
        model_name: 'beta-fast',
        request_count: 10,
        success_count: 7,
        failure_count: 3,
        success_rate: 70,
        avg_ttft_ms: 1_200,
        avg_tps: 42,
        buckets: createBuckets(70),
      },
    ],
    ...overrides,
  }
}

function response(snapshot = createSnapshot()): ModelStatusResponse {
  return { success: true, data: snapshot }
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <ModelStatusPage />
    </QueryClientProvider>
  )
}

function modelNames(): string[] {
  return screen
    .getAllByRole('article')
    .map((card) => within(card).getByRole('heading', { level: 2 }).textContent)
    .filter((name): name is string => Boolean(name))
}

describe('model status page', () => {
  beforeEach(() => {
    mocks.getModelStatus.mockReset()
    mocks.getModelStatus.mockResolvedValue(response())
  })

  test('shows models by request count and preserves missing metric semantics', async () => {
    renderPage()

    expect(
      await screen.findByRole('heading', { name: 'alpha-large' })
    ).toBeVisible()
    expect(modelNames()).toEqual(['alpha-large', 'beta-fast'])
    expect(
      within(screen.getByRole('article', { name: 'alpha-large' })).getAllByText(
        '—'
      )
    ).toHaveLength(2)
  })

  test('filters models by a case-insensitive search', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('heading', { name: 'alpha-large' })

    await user.type(
      screen.getByRole('searchbox', { name: 'Search models' }),
      'BETA'
    )

    expect(screen.queryByRole('heading', { name: 'alpha-large' })).toBeNull()
    expect(screen.getByRole('heading', { name: 'beta-fast' })).toBeVisible()
  })

  test('sorts success rate from lowest to highest', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('heading', { name: 'alpha-large' })

    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Sort models' }),
      'success-rate'
    )

    expect(modelNames()).toEqual(['beta-fast', 'alpha-large'])
  })

  test('uses compact directional labels for model sorting', async () => {
    renderPage()
    await screen.findByRole('heading', { name: 'alpha-large' })

    expect(
      screen.getByRole('option', { name: 'Request count ↓' })
    ).toBeVisible()
    expect(screen.getByRole('option', { name: 'Success rate ↑' })).toBeVisible()
    expect(
      screen.getByRole('option', { name: 'First token latency ↓' })
    ).toBeVisible()
  })

  test('requests a fresh snapshot when the selected group changes', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('heading', { name: 'alpha-large' })

    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Model group' }),
      'vip'
    )

    await waitFor(() => {
      expect(mocks.getModelStatus).toHaveBeenLastCalledWith('vip')
    })
  })

  test('distinguishes disabled collection, no activity, and no search matches', async () => {
    const user = userEvent.setup()
    mocks.getModelStatus.mockResolvedValueOnce(
      response(
        createSnapshot({
          collection_enabled: false,
          models: [],
        })
      )
    )
    const disabledView = renderPage()

    expect(
      await screen.findByText('Performance metrics collection is disabled')
    ).toBeVisible()

    disabledView.unmount()
    mocks.getModelStatus.mockResolvedValueOnce(
      response(createSnapshot({ models: [] }))
    )
    const emptyView = renderPage()
    expect(
      await screen.findByText('No model activity in the last 24 hours')
    ).toBeVisible()

    emptyView.unmount()
    mocks.getModelStatus.mockResolvedValueOnce(response())
    renderPage()
    await screen.findByRole('heading', { name: 'alpha-large' })
    await user.type(
      screen.getByRole('searchbox', { name: 'Search models' }),
      'missing-model'
    )
    expect(screen.getByText('No models match your search')).toBeVisible()
  })

  test('keeps filters available and retries after a request failure', async () => {
    const user = userEvent.setup()
    mocks.getModelStatus
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockResolvedValueOnce(response())
    renderPage()

    expect(await screen.findByText('Unable to load model status')).toBeVisible()
    expect(screen.getByRole('combobox', { name: 'Model group' })).toBeVisible()

    await user.click(screen.getByRole('button', { name: 'Try again' }))

    expect(
      await screen.findByRole('heading', { name: 'alpha-large' })
    ).toBeVisible()
    expect(mocks.getModelStatus).toHaveBeenCalledTimes(2)
  })
})

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
import { afterEach, beforeEach, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { LogDetailsDialog } from '../components/log-details-dialog'
import type { DegradationLogsResponse } from '../types'

const originalAdapter = api.defaults.adapter
const clients: QueryClient[] = []
let logsResult: unknown

function logs(): DegradationLogsResponse {
  return {
    success: true,
    message: '',
    data: {
      items: [
        {
          id: 1,
          created_at: '2026-10-05T12:00:00Z',
          channel_id: 1,
          channel_name: 'Test Channel',
          model_name: 'test-model',
          probe_id: 'probe',
          probe_name: 'Recorded Probe',
          status: 1,
          latency_ms: 250,
          token_count: 10,
          reasoning_tokens: 0,
          detail: '',
        },
      ],
      total: 1,
    },
  }
}

beforeEach(() => {
  logsResult = logs()
  api.defaults.adapter = async (config) => {
    const data = await logsResult
    if (data instanceof Error) throw data
    return { config, data, headers: {}, status: 200, statusText: 'OK' }
  }
})

afterEach(() => {
  api.defaults.adapter = originalAdapter
  clients.splice(0).forEach((client) => client.clear())
})

function renderLogsDialog() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>
      <LogDetailsDialog
        open
        onOpenChange={() => undefined}
        channelId={1}
        modelName='test-model'
        isAdmin
      />
    </QueryClientProvider>
  )
}

test.each([
  ['network', new Error('Network unavailable')],
  ['business', { success: false, message: 'Log storage unavailable' }],
])(
  'an initial %s logs failure shows an error and retry recovers',
  async (_, failure) => {
    const user = userEvent.setup()
    logsResult = failure
    renderLogsDialog()

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Failed to load logs'
    )
    expect(
      screen.queryByText('No probe logs recorded yet for this target.')
    ).toBeNull()

    logsResult = logs()
    await user.click(screen.getByRole('button', { name: 'Retry' }))

    expect(await screen.findByText('Recorded Probe')).toBeVisible()
    expect(screen.queryByRole('alert')).toBeNull()
  }
)

test.each([
  ['network', new Error('Network unavailable')],
  ['business', { success: false, message: 'Log storage unavailable' }],
])('a %s refresh failure retains logs with a warning', async (_, failure) => {
  const user = userEvent.setup()
  renderLogsDialog()
  await screen.findByText('Recorded Probe')
  logsResult = failure

  const heading = screen.getByRole('heading', {
    name: 'Degradation Probe Logs: test-model',
  })
  await user.click(
    within(heading.parentElement as HTMLElement).getByRole('button')
  )

  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Failed to refresh logs. Showing the last successful results.'
  )
  expect(screen.getByText('Recorded Probe')).toBeVisible()
  expect(
    screen.queryByText('No probe logs recorded yet for this target.')
  ).toBeNull()

  logsResult = logs()
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
})

test('successful logs with no records show the empty state', async () => {
  logsResult = { success: true, data: { items: [], total: 0 } }
  renderLogsDialog()

  expect(
    await screen.findByText('No probe logs recorded yet for this target.')
  ).toBeVisible()
  expect(screen.queryByRole('alert')).toBeNull()
})

test('keyboard users can focus, expand and collapse probe diagnostics', async () => {
  const user = userEvent.setup()
  const result = logs()
  result.data.items[0].detail = 'Expected 2 but received 3'
  result.data.items[0].reasoning_tokens = 128
  logsResult = result
  renderLogsDialog()

  const summary = await screen.findByRole('button', { name: /Recorded Probe/ })
  expect(summary).toHaveAttribute('aria-expanded', 'false')
  screen.getByRole('button', { name: 'Refresh' }).focus()
  await user.tab()
  expect(summary).toHaveFocus()
  await user.keyboard('{Enter}')

  expect(summary).toHaveAttribute('aria-expanded', 'true')
  const detail = screen.getByText('Expected 2 but received 3')
  expect(detail).toBeVisible()
  expect(screen.getByText('Reasoning Tokens: 128')).toBeVisible()
  const detailsId = summary.getAttribute('aria-controls')
  expect(detailsId).toBeTruthy()
  expect(document.querySelector(`[id="${detailsId}"]`)).toContainElement(detail)

  await user.click(detail)
  expect(detail).toBeVisible()
  summary.focus()
  await user.keyboard(' ')
  expect(summary).toHaveAttribute('aria-expanded', 'false')
  expect(screen.queryByText('Expected 2 but received 3')).toBeNull()
  expect(summary).toHaveFocus()
})

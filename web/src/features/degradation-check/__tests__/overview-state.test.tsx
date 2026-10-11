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
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MotionConfig } from 'motion/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { DegradationCheckPage } from '../index'
import type { DegradationOverviewResponse } from '../types'

const originalAdapter = api.defaults.adapter
const clients: QueryClient[] = []
let overviewResult: unknown

function overview(): DegradationOverviewResponse {
  return {
    success: true,
    message: '',
    data: {
      stats: {
        pass_rate: 100,
        total_executions: 0,
        mismatch_count: 0,
        error_count: 0,
        active_targets: 1,
        next_run_seconds: 300,
        enabled: true,
        aggregation_error: false,
      },
      channels: [
        {
          channel_id: 1,
          channel_name: 'Unexecuted Node',
          models: [
            {
              model_name: 'unexecuted-model',
              enabled: true,
              pass_rate: 100,
              total_executions: 0,
              avg_latency_ms: 0,
              probes: [],
            },
          ],
        },
      ],
    },
  }
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  localStorage.clear()
  useAuthStore.getState().auth.reset()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  overviewResult = overview()
  api.defaults.adapter = async (config) => {
    let data: unknown = { success: true, data: '' }
    if (config.url === '/api/degradation/overview') {
      data = await overviewResult
      if (data instanceof Error) throw data
    } else if (config.url === '/api/status') {
      data = { success: true, data: {} }
    }
    return { config, data, headers: {}, status: 200, statusText: 'OK' }
  }
})

afterEach(() => {
  api.defaults.adapter = originalAdapter
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  localStorage.clear()
})

async function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, retryDelay: 0 } },
  })
  clients.push(client)
  const root = createRootRoute()
  const route = createRoute({
    getParentRoute: () => root,
    path: '/',
    component: DegradationCheckPage,
  })
  const router = createRouter({
    routeTree: root.addChildren([route]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await act(async () => {
    await router.load()
  })
  return render(
    <QueryClientProvider client={client}>
      <MotionConfig reducedMotion='always'>
        <RouterProvider router={router} />
      </MotionConfig>
    </QueryClientProvider>
  )
}

test.each([
  ['Normal', 100],
  ['Degraded', 90],
  ['Error', 60],
] as const)(
  'the %s status filter excludes models that have never executed',
  async (filter, passRate) => {
    const response = overview()
    response.data.channels[0].models[0].pass_rate = passRate
    overviewResult = response
    const user = userEvent.setup()
    await renderPage()
    await screen.findAllByText('unexecuted-model')

    await user.click(screen.getByRole('button', { name: filter }))

    expect(screen.queryByText('unexecuted-model')).toBeNull()
    expect(screen.getByText('No Model Degradation Data Found')).toBeVisible()
  }
)

test('an executed model stays critical when another node has no executions', async () => {
  const response = overview()
  response.data.channels.push({
    channel_id: 2,
    channel_name: 'Executed Node',
    models: [
      {
        ...response.data.channels[0].models[0],
        pass_rate: 60,
        total_executions: 5,
        avg_latency_ms: 800,
      },
    ],
  })
  overviewResult = response
  const user = userEvent.setup()
  await renderPage()
  await screen.findAllByText('unexecuted-model')

  await user.click(screen.getByRole('button', { name: 'Error' }))

  expect(screen.getAllByText('unexecuted-model')[0]).toBeVisible()
  expect(screen.getAllByText('60.0%')[0]).toBeVisible()
})

test.each([
  ['network', new Error('Network unavailable')],
  ['business', { success: false, message: 'Overview unavailable' }],
])(
  'an initial %s failure shows an error and retry can recover',
  async (_, failure) => {
    const user = userEvent.setup()
    overviewResult = failure
    await renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Failed to load degradation data.'
    )
    expect(screen.queryByText('No Model Degradation Data Found')).toBeNull()
    expect(screen.queryByText('Average Pass Rate')).toBeNull()

    overviewResult = overview()
    await user.click(screen.getByRole('button', { name: 'Retry' }))

    expect((await screen.findAllByText('unexecuted-model'))[0]).toBeVisible()
    expect(screen.queryByRole('alert')).toBeNull()
  }
)

test.each([
  ['network', new Error('Network unavailable')],
  ['business', { success: false, message: 'Overview unavailable' }],
])(
  'a %s refresh failure warns that retained data is stale',
  async (_, failure) => {
    const user = userEvent.setup()
    await renderPage()
    await screen.findAllByText('unexecuted-model')
    overviewResult = failure

    const header = screen
      .getByRole('heading', {
        name: 'Degradation Check',
      })
      .closest('header')
    expect(header).not.toBeNull()
    await user.click(within(header as HTMLElement).getByRole('button'))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Failed to refresh degradation data. Showing the last successful results.'
    )
    expect(screen.getAllByText('unexecuted-model')[0]).toBeVisible()
    overviewResult = overview()
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
  }
)

test('an empty successful overview retains its normal empty state', async () => {
  const response = overview()
  response.data.channels = []
  overviewResult = response
  await renderPage()

  expect(
    await screen.findByText('No Model Degradation Data Found')
  ).toBeVisible()
  expect(screen.queryByRole('alert')).toBeNull()
  expect(screen.getByText('Average Pass Rate')).toBeVisible()
})

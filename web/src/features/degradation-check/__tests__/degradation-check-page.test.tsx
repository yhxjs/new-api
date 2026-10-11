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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { DegradationCheckPage } from '../index'
import type { DegradationOverviewResponse } from '../types'

const mocks = vi.hoisted(() => ({
  getDegradationOverview: vi.fn(),
  triggerDegradationRun: vi.fn(),
  triggerDegradationTargetRun: vi.fn(),
}))

vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: React.ReactNode }) => props.children,
}))

vi.mock('@/components/page-transition', () => ({
  PageTransition: (props: { children: React.ReactNode }) => props.children,
}))

vi.mock('@tanstack/react-router', () => ({
  Link: (props: { children: React.ReactNode }) => <a>{props.children}</a>,
}))

vi.mock('../api', () => ({
  getDegradationOverview: mocks.getDegradationOverview,
  triggerDegradationRun: mocks.triggerDegradationRun,
  triggerDegradationTargetRun: mocks.triggerDegradationTargetRun,
}))

const mockAuthStore = vi.hoisted(() => ({
  user: {
    id: 1,
    username: 'admin',
    role: 10,
    permissions: {
      admin_permissions: { channel: { read: true, operate: true } },
    },
  },
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (
    selector: (state: { auth: { user: typeof mockAuthStore.user } }) => unknown
  ) => selector({ auth: { user: mockAuthStore.user } }),
}))

function createOverviewResponse(): DegradationOverviewResponse {
  return {
    success: true,
    message: '',
    data: {
      stats: {
        pass_rate: 98.5,
        total_executions: 240,
        mismatch_count: 2,
        error_count: 1,
        active_targets: 4,
        next_run_seconds: 300,
        enabled: true,
        aggregation_error: false,
      },
      channels: [
        {
          channel_id: 1,
          channel_name: 'OpenAI Primary',
          models: [
            {
              model_name: 'gpt-4o',
              enabled: true,
              reasoning_effort: 'medium',
              pass_rate: 99.0,
              total_executions: 100,
              avg_latency_ms: 650,
              probes: [
                {
                  probe_id: 'probe_1',
                  probe_name: 'Logic Reasoning',
                  pass_rate: 99.0,
                  slots: Array(24).fill(1),
                },
              ],
            },
            {
              model_name: 'o3-mini',
              enabled: true,
              reasoning_effort: 'high',
              pass_rate: 85.0,
              total_executions: 100,
              avg_latency_ms: 1200,
              probes: [
                {
                  probe_id: 'probe_2',
                  probe_name: 'Math Probe',
                  pass_rate: 85.0,
                  slots: Array(24).fill(2),
                },
              ],
            },
          ],
        },
        {
          channel_id: 2,
          channel_name: 'Anthropic Official',
          models: [
            {
              model_name: 'claude-3-7-sonnet',
              enabled: true,
              reasoning_effort: 'off',
              pass_rate: 100.0,
              total_executions: 40,
              avg_latency_ms: 780,
              probes: [
                {
                  probe_id: 'probe_1',
                  probe_name: 'Logic Reasoning',
                  pass_rate: 100.0,
                  slots: Array(24).fill(1),
                },
              ],
            },
          ],
        },
      ],
    },
  }
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <DegradationCheckPage />
    </QueryClientProvider>
  )
}

describe('degradation check dashboard page', () => {
  beforeEach(() => {
    mocks.getDegradationOverview.mockReset()
    mocks.getDegradationOverview.mockResolvedValue(createOverviewResponse())
    mocks.triggerDegradationTargetRun.mockReset()
    mocks.triggerDegradationTargetRun.mockResolvedValue({
      success: true,
      message: '',
    })
    mockAuthStore.user = {
      id: 1,
      username: 'admin',
      role: 100,
      permissions: {
        admin_permissions: { channel: { read: true, operate: true } },
      },
    }
  })

  test('renders top statistics and model cards correctly', async () => {
    renderPage()

    expect(await screen.findByText('98.5%')).toBeVisible()
    expect(screen.getByText('240')).toBeVisible()
    expect(screen.getByText('gpt-4o')).toBeVisible()
    expect(screen.getByText('claude-3-7-sonnet')).toBeVisible()
    expect(screen.getByText('o3-mini')).toBeVisible()
  })

  test('filters models by search query', async () => {
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('gpt-4o')
    const searchInput = screen.getByPlaceholderText(
      'Search model or node name...'
    )
    await user.type(searchInput, 'claude')

    expect(screen.queryByText('gpt-4o')).toBeNull()
    expect(screen.getByText('claude-3-7-sonnet')).toBeVisible()
  })

  test('shows the aggregate timeline when a model has no applicable probes', async () => {
    const response = createOverviewResponse()
    response.data.channels = [response.data.channels[0]]
    response.data.channels[0].models = [response.data.channels[0].models[0]]
    response.data.channels[0].models[0].probes = []
    mocks.getDegradationOverview.mockResolvedValue(response)

    renderPage()

    expect(await screen.findByText('gpt-4o')).toBeVisible()
    expect(screen.getByText('Aggregated Probes')).toBeVisible()
  })

  test('weights model health by execution count across channels when filtering', async () => {
    const user = userEvent.setup()
    const response = createOverviewResponse()
    const frequentModel = response.data.channels[0].models[0]
    frequentModel.pass_rate = 100
    frequentModel.total_executions = 19
    response.data.channels[0].models = [frequentModel]
    response.data.channels[1].models = [
      { ...frequentModel, pass_rate: 0, total_executions: 1 },
    ]
    mocks.getDegradationOverview.mockResolvedValue(response)

    renderPage()

    expect(await screen.findByText('95.0%')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Normal' }))
    expect(screen.getByText('gpt-4o')).toBeVisible()
  })

  test('filters models by status tab', async () => {
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('gpt-4o')

    // Click 'Degraded' status tab (o3-mini has 85% pass rate so it's in degraded range)
    const degradedBtn = screen.getByRole('button', { name: 'Degraded' })
    await user.click(degradedBtn)

    expect(screen.queryByText('claude-3-7-sonnet')).toBeNull()
    expect(screen.getByText('o3-mini')).toBeVisible()
  })

  test('shows admin-only features for admin role and hides them for standard users', async () => {
    // Admin view
    const adminView = renderPage()
    expect(await screen.findByText('Configure Probes')).toBeVisible()
    expect(screen.getByText('Run Check Now')).toBeVisible()
    adminView.unmount()

    // Non-admin view
    mockAuthStore.user = {
      id: 2,
      username: 'user',
      role: 1,
      permissions: {
        admin_permissions: { channel: { read: false, operate: false } },
      },
    }
    renderPage()
    expect(screen.queryByText('Configure Probes')).toBeNull()
    expect(screen.queryByText('Run Check Now')).toBeNull()
  })

  test('hides operation controls when an admin lacks channel permissions', async () => {
    mockAuthStore.user = {
      id: 3,
      username: 'restricted-admin',
      role: 10,
      permissions: {
        admin_permissions: { channel: { read: false, operate: false } },
      },
    }
    renderPage()

    await screen.findByText('gpt-4o')
    expect(screen.queryByText('Run Check Now')).toBeNull()
    expect(screen.queryByText('Probe')).toBeNull()
    expect(screen.queryByText('Logs')).toBeNull()
  })

  test('keeps root-only configuration hidden from channel operators', async () => {
    mockAuthStore.user = {
      id: 4,
      username: 'channel-operator',
      role: 10,
      permissions: {
        admin_permissions: { channel: { read: true, operate: true } },
      },
    }
    renderPage()

    await screen.findByText('gpt-4o')
    expect(screen.getByText('Run Check Now')).toBeVisible()
    expect(screen.queryByText('Configure Probes')).toBeNull()
  })

  test.each([0, undefined])(
    'disables targeted probes for an operator when the channel ID is masked as %s',
    async (channelId) => {
      const user = userEvent.setup()
      mockAuthStore.user.role = 10
      mockAuthStore.user.permissions.admin_permissions.channel.read = false
      const response = createOverviewResponse()
      const channel = response.data.channels[0]
      channel.channel_id = channelId
      channel.channel_name = 'Node 1'
      channel.models = [channel.models[0]]
      response.data.channels = [channel]
      mocks.getDegradationOverview.mockResolvedValue(response)

      renderPage()

      const probeButton = await screen.findByRole('button', { name: 'Probe' })
      expect(probeButton).toBeDisabled()
      await user.click(probeButton)
      expect(mocks.triggerDegradationTargetRun).not.toHaveBeenCalled()
      expect(
        screen.getByRole('button', { name: 'Run Check Now' })
      ).toBeEnabled()
    }
  )

  test('allows an operator to probe a channel when its ID is available', async () => {
    const user = userEvent.setup()
    mockAuthStore.user.role = 10
    mockAuthStore.user.permissions.admin_permissions.channel.read = false
    const response = createOverviewResponse()
    const channel = response.data.channels[0]
    channel.models = [channel.models[0]]
    response.data.channels = [channel]
    mocks.getDegradationOverview.mockResolvedValue(response)

    renderPage()

    const probeButton = await screen.findByRole('button', { name: 'Probe' })
    expect(probeButton).toBeEnabled()
    await user.click(probeButton)
    expect(mocks.triggerDegradationTargetRun).toHaveBeenCalledWith(1, 'gpt-4o')
  })
})

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
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { CHANNEL_TYPE_NEW_API } from '../../../constants'
import { channelsQueryKeys } from '../../../lib'
import { channelSchema } from '../../../types'
import { ChannelsProvider } from '../../channels-provider'
import { ChannelMutateDrawer } from '../channel-mutate-drawer'

const previousUser = useAuthStore.getState().auth.user
let queryClient: QueryClient

afterEach(() => {
  cleanup()
  queryClient?.clear()
  useAuthStore.getState().auth.setUser(previousUser)
  localStorage.removeItem('channel-advanced-settings-expanded')
})

test.each([
  { name: 'default disabled', settings: '{}', configured: false },
  {
    name: 'explicitly disabled',
    settings: '{"balance_query":{"mode":"disabled"}}',
    configured: false,
  },
  {
    name: 'subscription',
    settings: '{"balance_query":{"mode":"subscription"}}',
    configured: true,
  },
])(
  '$name balance query shows the correct navigation status',
  async (testCase) => {
    const channel = channelSchema.parse({
      id: 1,
      type: CHANNEL_TYPE_NEW_API,
      key: '',
      status: 1,
      name: 'Balance query upstream',
      created_time: 0,
      test_time: 0,
      response_time: 0,
      balance_updated_time: 0,
      base_url: 'https://upstream.example',
      models: 'gpt-test',
      settings: testCase.settings,
    })
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    queryClient.setQueryData(channelsQueryKeys.detail(channel.id), {
      success: true,
      data: channel,
    })
    queryClient.setQueryData(['groups'], { success: true, data: ['default'] })
    queryClient.setQueryData(['channel_models'], {
      success: true,
      data: [{ id: 'gpt-test' }],
    })
    queryClient.setQueryData(['prefill_groups', 'model'], {
      success: true,
      data: [],
    })
    useAuthStore
      .getState()
      .auth.setUser({ id: 1, username: 'root', role: ROLE.SUPER_ADMIN })
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/user/2fa/status' || url === '/api/user/passkey') {
        return { data: { success: true, data: { enabled: false } } }
      }
      throw new Error(`Unexpected request: ${url}`)
    })
    const user = userEvent.setup()
    render(
      <QueryClientProvider client={queryClient}>
        <ChannelsProvider>
          <ChannelMutateDrawer
            open
            currentRow={channel}
            onOpenChange={vi.fn()}
          />
        </ChannelsProvider>
      </QueryClientProvider>
    )

    const navigation = within(
      screen.getByRole('navigation', { name: 'Channels' })
    )
    await user.click(
      navigation.getByRole('button', { name: /^Advanced Settings/ })
    )
    const balanceQuery = navigation.getByRole('button', {
      name: 'Balance Query',
    })

    if (testCase.configured) {
      expect(balanceQuery).toHaveClass('text-primary')
    } else {
      expect(balanceQuery).not.toHaveClass('text-primary')
    }
  }
)

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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
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

test('a fractional quota per USD passes native validation and saves', async () => {
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
    group: 'default',
    settings: JSON.stringify({
      balance_query: { mode: 'user_api', user_id: '1' },
    }),
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
  localStorage.setItem('channel-advanced-settings-expanded', 'true')
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/user/2fa/status' || url === '/api/user/passkey') {
      return { data: { success: true, data: { enabled: false } } }
    }
    if (url === '/api/channel/1') {
      return { data: { success: true, data: channel } }
    }
    throw new Error(`Unexpected request: ${url}`)
  })
  const update = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={queryClient}>
      <ChannelsProvider>
        <ChannelMutateDrawer open currentRow={channel} onOpenChange={vi.fn()} />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  const quotaInput = await screen.findByRole('spinbutton', {
    name: 'Quota per USD',
  })

  await user.type(quotaInput, '0.5')

  expect(quotaInput).toBeValid()
  await user.click(screen.getByRole('button', { name: 'Update Channel' }))
  await waitFor(() => expect(update).toHaveBeenCalled())
  const payload = update.mock.calls[0][1] as { settings: string }
  expect(JSON.parse(payload.settings).balance_query.quota_per_unit).toBe(0.5)
})

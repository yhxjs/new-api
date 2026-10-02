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
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
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
  {
    name: 'revealing a token then saving a name omits the unchanged token',
    editedToken: null,
  },
  {
    name: 'editing a token after revealing it submits the replacement',
    editedToken: 'pat-replacement',
  },
])(
  '$name',
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
    vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: true,
        data: { key: 'sk-test', balance_query_access_token: 'pat-old' },
      },
    })
    const update = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
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

    const accessToken = await screen.findByLabelText('Access Token')
    expect(accessToken).toHaveValue('')
    await user.click(screen.getByRole('button', { name: 'Reveal key' }))
    await waitFor(() => expect(accessToken).toHaveValue('pat-old'))
    if (testCase.editedToken !== null) {
      await user.clear(accessToken)
      await user.paste(testCase.editedToken)
    }
    const name = screen.getByRole('textbox', { name: 'Name *' })
    await user.clear(name)
    await user.paste('Renamed upstream')
    await user.click(screen.getByRole('button', { name: 'Update Channel' }))

    await waitFor(() => expect(update).toHaveBeenCalled())
    const payload = update.mock.calls[0][1] as {
      name: string
      settings: string
    }
    expect(payload.name).toBe('Renamed upstream')
    expect(JSON.parse(payload.settings).balance_query).toEqual({
      mode: 'user_api',
      user_id: '1',
      ...(testCase.editedToken === null
        ? {}
        : { access_token: testCase.editedToken }),
    })
  },
  15000
)

test.each([
  'unchanged',
  'replace before reveal',
  'clear after reveal',
] as const)(
  'custom request reveal preserves %s edits and saves only intended changes',
  async (action) => {
    const request = {
      url: 'https://upstream.example/balance?token=pat-url',
      headers: { Authorization: 'Bearer pat-header' },
      body: 'pat-body',
    }
    const redacted = {
      url: '[REDACTED]',
      headers: { Authorization: '[REDACTED]' },
      body: '[REDACTED]',
    }
    const channel = channelSchema.parse({
      id: 1,
      type: CHANNEL_TYPE_NEW_API,
      key: '',
      status: 1,
      name: 'Custom upstream',
      created_time: 0,
      test_time: 0,
      response_time: 0,
      balance_updated_time: 0,
      base_url: 'https://upstream.example',
      models: 'gpt-test',
      group: 'default',
      settings: JSON.stringify({
        balance_query: {
          mode: 'custom',
          method: 'POST',
          extract: 'response.balance',
          ...redacted,
        },
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
    vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: true,
        data: { key: 'sk-test', balance_query_request: request },
      },
    })
    const update = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
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
    const url = await screen.findByRole('textbox', { name: 'URL' })
    const headers = screen.getByRole('textbox', { name: 'Headers' })
    const body = screen.getByRole('textbox', { name: 'Request Body' })
    expect(url).toHaveValue('[REDACTED]')
    if (action === 'replace before reveal') {
      fireEvent.input(headers, {
        target: { value: '{"Authorization":"Bearer pat-new"}' },
      })
    }
    await user.click(screen.getByRole('button', { name: 'Reveal key' }))
    await waitFor(() => expect(url).toHaveValue(request.url))
    expect(body).toHaveValue(request.body)
    if (action === 'replace before reveal') {
      expect(headers).toHaveValue('{"Authorization":"Bearer pat-new"}')
    } else {
      expect(JSON.parse((headers as HTMLTextAreaElement).value)).toEqual(
        request.headers
      )
    }
    if (action === 'clear after reveal') {
      fireEvent.input(headers, { target: { value: '{}' } })
      await user.clear(body)
    }
    await user.click(screen.getByRole('button', { name: 'Update Channel' }))
    await waitFor(() => expect(update).toHaveBeenCalled())
    const payload = update.mock.calls[0][1] as { settings: string }
    const saved = JSON.parse(payload.settings).balance_query
    expect(saved.url).toBe('[REDACTED]')
    if (action === 'replace before reveal') {
      expect(saved.headers).toEqual({ Authorization: 'Bearer pat-new' })
    } else if (action === 'clear after reveal') {
      expect(saved.headers).toBeUndefined()
      expect(saved.body).toBeUndefined()
    } else {
      expect(saved.headers).toEqual(redacted.headers)
    }
    if (action !== 'clear after reveal') expect(saved.body).toBe('[REDACTED]')
  },
  15000
)

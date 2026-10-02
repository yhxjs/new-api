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
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { CHANNEL_TYPE_NEW_API } from '../../../constants'
import { channelsQueryKeys } from '../../../lib'
import { channelSchema, type Channel } from '../../../types'
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
  'another channel',
  'the same channel reopened',
  'a new channel',
  'the same channel with a newer reveal',
] as const)(
  'a pending reveal cannot populate credentials in %s',
  async (destination) => {
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
          method: 'GET',
          url: '[REDACTED]',
          headers: { Authorization: '[REDACTED]' },
          extract: 'response.balance',
        },
      }),
    })
    const otherChannel = { ...channel, id: 2, name: 'Other upstream' }
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    for (const row of [channel, otherChannel]) {
      queryClient.setQueryData(channelsQueryKeys.detail(row.id), {
        success: true,
        data: row,
      })
    }
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
      throw new Error(`Unexpected request: ${url}`)
    })
    let resolveReveal!: (response: {
      data: {
        success: boolean
        data: {
          key: string
          balance_query_request: {
            url: string
            headers: Record<string, string>
            body: string
          }
        }
      }
    }) => void
    const pendingReveal = new Promise<Parameters<typeof resolveReveal>[0]>(
      (resolve) => {
        resolveReveal = resolve
      }
    )
    const reveal = vi.spyOn(api, 'post').mockReturnValueOnce(pendingReveal)
    const user = userEvent.setup()
    const drawer = (open: boolean, row: Channel | null) => (
      <QueryClientProvider client={queryClient}>
        <ChannelsProvider>
          <ChannelMutateDrawer
            open={open}
            currentRow={row}
            onOpenChange={vi.fn()}
          />
        </ChannelsProvider>
      </QueryClientProvider>
    )
    const rendered = render(drawer(true, channel))
    await screen.findByRole('textbox', { name: 'URL' })
    await user.click(screen.getByRole('button', { name: 'Reveal key' }))
    await waitFor(() =>
      expect(reveal).toHaveBeenCalledWith(
        '/api/channel/1/key',
        undefined,
        expect.anything()
      )
    )
    rendered.rerender(drawer(false, null))
    let nextChannel: Channel | null = channel
    if (destination === 'a new channel') nextChannel = null
    if (destination === 'another channel') nextChannel = otherChannel
    rendered.rerender(drawer(true, nextChannel))
    if (!nextChannel) {
      const type = screen.getByPlaceholderText('Search channel type...')
      await user.clear(type)
      await user.type(type, String(CHANNEL_TYPE_NEW_API))
      await user.keyboard('{Enter}')
      await user.click(
        screen.getByRole('button', {
          name: /^Advanced Settings Request overrides/,
        })
      )
      await user.click(
        screen.getByRole('combobox', { name: 'Balance Query Mode' })
      )
      await user.click(screen.getByRole('option', { name: 'Custom' }))
    }
    const url = await screen.findByRole('textbox', { name: 'URL' })
    let resolveNewerReveal: (() => void) | undefined
    if (destination === 'the same channel with a newer reveal') {
      const response = { data: { success: true, data: { key: 'sk-current' } } }
      const newerReveal = new Promise<typeof response>((resolve) => {
        resolveNewerReveal = () => resolve(response)
      })
      reveal.mockReturnValueOnce(newerReveal)
      await user.click(screen.getByRole('button', { name: 'Reveal key' }))
      await waitFor(() => expect(reveal).toHaveBeenCalledTimes(2))
    }
    await act(async () => {
      resolveReveal({
        data: {
          success: true,
          data: {
            key: 'sk-old',
            balance_query_request: {
              url: 'https://old.example/balance?token=old-secret',
              headers: { Authorization: 'Bearer old-secret' },
              body: '',
            },
          },
        },
      })
      await pendingReveal
    })
    expect(url).toHaveValue(nextChannel ? '[REDACTED]' : '')
    const headers = screen.getByRole('textbox', {
      name: 'Headers',
    }) as HTMLTextAreaElement
    expect(JSON.parse(headers.value || '{}')).toEqual(
      nextChannel ? { Authorization: '[REDACTED]' } : {}
    )
    if (resolveNewerReveal) {
      expect(screen.getByRole('button', { name: 'Reveal key' })).toBeDisabled()
      await act(async () => resolveNewerReveal?.())
      await waitFor(() =>
        expect(
          screen.getByRole('button', { name: 'Reveal key' })
        ).not.toBeDisabled()
      )
      expect(
        screen.getByPlaceholderText('Hidden — verify to reveal')
      ).toHaveValue('sk-current')
    } else if (nextChannel) {
      expect(
        screen.getByRole('button', { name: 'Reveal key' })
      ).not.toBeDisabled()
    }
  },
  15000
)

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
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { CHANNEL_TYPE_NEW_API } from '../../../constants'
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
    mode: 'Custom',
    error: 'Extract expression is required for custom balance queries',
    field: 'Extract Expression',
  },
  {
    mode: 'New API /api/user/self',
    error: 'Access token is required for user API balance queries',
    field: 'Access Token',
  },
])(
  'saving $mode with missing required fields expands their hidden errors',
  async (testCase) => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
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
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { enabled: false } },
    })
    const create = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { success: true } })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(
      function (this: HTMLElement) {
        return new DOMRect(0, this.id === 'channel-form' ? 0 : 200, 100, 100)
      }
    )
    const user = userEvent.setup()
    render(
      <QueryClientProvider client={queryClient}>
        <ChannelsProvider>
          <ChannelMutateDrawer open currentRow={null} onOpenChange={vi.fn()} />
        </ChannelsProvider>
      </QueryClientProvider>
    )
    await user.type(
      screen.getByRole('textbox', { name: 'Name *' }),
      'New upstream'
    )
    const type = screen.getByPlaceholderText('Search channel type...')
    await user.clear(type)
    await user.type(type, String(CHANNEL_TYPE_NEW_API))
    await user.keyboard('{Enter}')
    await user.type(
      screen.getByRole('textbox', { name: 'Base URL' }),
      'https://upstream.example'
    )
    await user.type(
      screen.getByRole('textbox', { name: 'API Key *' }),
      'sk-test'
    )
    await user.click(screen.getByRole('button', { name: 'Fill All Models' }))
    const advanced = screen.getByRole('button', {
      name: /^Advanced Settings Request overrides/,
    })
    await user.click(advanced)
    await user.click(
      screen.getByRole('combobox', { name: 'Balance Query Mode' })
    )
    await user.click(screen.getByRole('option', { name: testCase.mode }))
    if (testCase.mode !== 'Custom') {
      await user.type(screen.getByRole('textbox', { name: 'User ID' }), '42')
    }
    await user.click(advanced)
    expect(advanced).toHaveAttribute('aria-expanded', 'false')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))

    expect(await screen.findByText(testCase.error)).toBeVisible()
    expect(advanced).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByLabelText(testCase.field)).toHaveAttribute(
      'aria-invalid',
      'true'
    )
    expect(create).not.toHaveBeenCalled()
  },
  15000
)

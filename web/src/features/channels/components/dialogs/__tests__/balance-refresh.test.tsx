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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useEffect } from 'react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { CHANNEL_TYPE_NEW_API } from '../../../constants'
import { channelSchema, type Channel } from '../../../types'
import { ChannelsProvider, useChannels } from '../../channels-provider'
import { BalanceQueryDialog } from '../balance-query-dialog'

let queryClient: QueryClient

afterEach(() => {
  queryClient?.clear()
})

function BalanceDialogFixture(props: { channel: Channel }) {
  const { setCurrentRow } = useChannels()
  useEffect(() => {
    setCurrentRow(props.channel)
  }, [props.channel, setCurrentRow])
  return <BalanceQueryDialog open onOpenChange={() => undefined} />
}

test('a successful manual balance refresh removes an earlier failure warning', async () => {
  const channel = channelSchema.parse({
    id: 1,
    type: CHANNEL_TYPE_NEW_API,
    key: '',
    status: 1,
    name: 'Balance upstream',
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance: 7,
    balance_updated_time: 1,
    channel_info: { balance_query_last_failed_time: 2 },
  })
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, balance: 0.5 },
  })
  const user = userEvent.setup()
  render(
    <QueryClientProvider client={queryClient}>
      <ChannelsProvider>
        <BalanceDialogFixture channel={channel} />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  const warning = await screen.findByText(
    /Automatic balance refreshes have been failing/
  )

  await user.click(screen.getByRole('button', { name: 'Update Balance' }))

  await waitFor(() => expect(warning).not.toBeInTheDocument())
})

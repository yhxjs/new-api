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
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { getUserRankings } from '../../api'
import { useUserRankings } from '../use-rankings'

vi.mock('../../api', () => ({
  getRankings: vi.fn(),
  getUserRankings: vi.fn(),
}))

const queryClients: QueryClient[] = []

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClients.push(queryClient)

  return function Wrapper(props: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        {props.children}
      </QueryClientProvider>
    )
  }
}

afterEach(() => {
  for (const queryClient of queryClients) queryClient.clear()
  queryClients.length = 0
})

describe('useUserRankings', () => {
  test('does not request data while the feature is disabled', () => {
    renderHook(() => useUserRankings('week', false, false), {
      wrapper: createWrapper(),
    })

    expect(getUserRankings).not.toHaveBeenCalled()
  })

  test('requests a fresh identity-specific query after login state changes', async () => {
    vi.mocked(getUserRankings).mockResolvedValue({
      success: true,
      data: { users: [] },
    })

    const hook = renderHook(
      (props: { authenticated: boolean }) =>
        useUserRankings('week', true, props.authenticated),
      {
        initialProps: { authenticated: false },
        wrapper: createWrapper(),
      }
    )

    await waitFor(() => expect(getUserRankings).toHaveBeenCalledTimes(1))

    hook.rerender({ authenticated: true })

    await waitFor(() => expect(getUserRankings).toHaveBeenCalledTimes(2))
  })
})

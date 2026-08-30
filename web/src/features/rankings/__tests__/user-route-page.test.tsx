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
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { UserRankingsPage } from '../user-rankings-page'

const mocks = vi.hoisted(() => ({
  useRankings: vi.fn(),
  useUserRankings: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  useSearch: () => ({ period: 'month' }),
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ auth: { user: null } }),
}))

vi.mock('../hooks/use-rankings', () => ({
  useRankings: mocks.useRankings,
  useUserRankings: mocks.useUserRankings,
}))

vi.mock('../components', () => ({
  UserRankingsSection: () => <div>user route rankings</div>,
}))

describe('user rankings route page', () => {
  beforeEach(() => {
    mocks.useRankings.mockReset()
    mocks.useUserRankings.mockReset()
    mocks.useUserRankings.mockReturnValue({
      data: { data: { users: [] } },
      error: null,
      isLoading: false,
    })
  })

  test('the user route never starts the overall rankings query', () => {
    render(<UserRankingsPage />)

    expect(screen.getByText('user route rankings')).toBeInTheDocument()
    expect(mocks.useRankings).not.toHaveBeenCalled()
  })
})

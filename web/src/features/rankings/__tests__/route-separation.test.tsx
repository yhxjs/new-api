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

import { Rankings } from '../index'

const mocks = vi.hoisted(() => ({
  useRankings: vi.fn(),
  useUserRankings: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => vi.fn(),
  useSearch: () => ({ period: 'week' }),
}))

vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: React.ReactNode }) => props.children,
}))

vi.mock('@/components/page-transition', () => ({
  PageTransition: (props: { children: React.ReactNode }) => props.children,
}))

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({
    status: {
      HeaderNavModules:
        '{"rankings":{"enabled":true,"requireAuth":false,"userLeaderboardEnabled":true}}',
    },
  }),
}))

vi.mock('@/lib/nav-modules', () => ({
  isUserLeaderboardEnabledFromStatus: () => true,
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
  MarketShareSection: () => <div>overall market share</div>,
  ModelsSection: () => <div>overall models</div>,
  PulseSection: () => <div>overall pulse</div>,
  RankingsHero: () => null,
  UserRankingsSection: () => <div>user rankings</div>,
}))

describe('rankings route separation', () => {
  beforeEach(() => {
    mocks.useRankings.mockReset()
    mocks.useUserRankings.mockReset()
    mocks.useRankings.mockReturnValue({
      data: {
        data: {
          models: [],
          vendors: [],
          top_movers: [],
          top_droppers: [],
          models_history: { points: [], models: [], buckets: 0 },
          vendor_share_history: { points: [], vendors: [], buckets: 0 },
        },
      },
      error: null,
      isLoading: false,
    })
    mocks.useUserRankings.mockReturnValue({
      data: { data: { users: [] } },
      error: null,
      isLoading: false,
    })
  })

  test('the overall route never starts the user rankings query', () => {
    render(<Rankings />)

    expect(screen.getByText('overall models')).toBeInTheDocument()
    expect(mocks.useUserRankings).not.toHaveBeenCalled()
  })
})

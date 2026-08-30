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
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { Route as RankingsRoute } from '../../../routes/rankings/route'
import { Route as UserRankingsRoute } from '../../../routes/rankings/users'

const mocks = vi.hoisted(() => ({
  getFreshModuleAccess: vi.fn(),
  getState: vi.fn(),
  redirect: vi.fn((options: Record<string, unknown>) =>
    Object.assign(new Error('redirect'), { options })
  ),
}))

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => (options: Record<string, unknown>) => ({ options }),
  redirect: mocks.redirect,
}))

vi.mock('@/features/rankings/layout', () => ({
  RankingsLayout: () => null,
}))

vi.mock('@/features/rankings/user-rankings-page', () => ({
  UserRankingsPage: () => null,
}))

vi.mock('@/lib/nav-modules', () => ({
  getFreshModuleAccess: mocks.getFreshModuleAccess,
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: { getState: mocks.getState },
}))

type BeforeLoad = (args: {
  context: {
    rankingsAccess?: {
      enabled: boolean
      requireAuth: boolean
      userLeaderboardEnabled: boolean
    }
  }
  location: { href: string }
  search: { period?: 'today' | 'week' | 'month' | 'year' }
}) => unknown

function getBeforeLoad(route: unknown): BeforeLoad {
  return (
    route as {
      options: { beforeLoad: BeforeLoad }
    }
  ).options.beforeLoad
}

describe('rankings route access', () => {
  beforeEach(() => {
    mocks.getFreshModuleAccess.mockReset()
    mocks.getState.mockReset()
    mocks.redirect.mockClear()
  })

  test('the parent route exposes the complete rankings access configuration', async () => {
    const access = {
      enabled: true,
      requireAuth: false,
      userLeaderboardEnabled: true,
    }
    mocks.getFreshModuleAccess.mockResolvedValue(access)

    const result = await getBeforeLoad(RankingsRoute)({
      context: {},
      location: { href: '/rankings?period=month' },
      search: { period: 'month' },
    })

    expect(result).toEqual({ rankingsAccess: access })
  })

  test('the user route redirects to the overall route when its setting is off', () => {
    const beforeLoad = getBeforeLoad(UserRankingsRoute)

    expect(() =>
      beforeLoad({
        context: {
          rankingsAccess: {
            enabled: true,
            requireAuth: false,
            userLeaderboardEnabled: false,
          },
        },
        location: { href: '/rankings/users?period=month' },
        search: { period: 'month' },
      })
    ).toThrow('redirect')
    expect(mocks.redirect).toHaveBeenCalledWith({
      to: '/rankings',
      search: { period: 'month' },
      replace: true,
    })
  })

  test('the user route remains accessible when its setting is on', () => {
    const beforeLoad = getBeforeLoad(UserRankingsRoute)

    expect(
      beforeLoad({
        context: {
          rankingsAccess: {
            enabled: true,
            requireAuth: false,
            userLeaderboardEnabled: true,
          },
        },
        location: { href: '/rankings/users?period=week' },
        search: { period: 'week' },
      })
    ).toBeUndefined()
    expect(mocks.redirect).not.toHaveBeenCalled()
  })
})

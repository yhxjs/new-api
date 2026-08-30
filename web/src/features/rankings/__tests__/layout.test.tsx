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
import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { RankingsLayout } from '../layout'

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  pathname: '/rankings/users',
}))

vi.mock('@tanstack/react-router', () => ({
  Outlet: () => <div>active child route</div>,
  useNavigate: () => mocks.navigate,
  useRouterState: (options: {
    select: (state: { location: { pathname: string } }) => unknown
  }) => options.select({ location: { pathname: mocks.pathname } }),
  useSearch: () => ({ period: 'month' }),
}))

vi.mock('@/components/layout', () => ({
  PublicLayout: (props: { children: React.ReactNode }) => props.children,
}))

vi.mock('@/components/page-transition', () => ({
  PageTransition: (props: { children: React.ReactNode }) => props.children,
}))

vi.mock('../components', () => ({
  RankingsHero: (props: {
    activeView: string
    onPeriodChange: (period: 'year') => void
    userLeaderboardEnabled: boolean
  }) => (
    <div>
      <span>{`view:${props.activeView}`}</span>
      <span>{`users:${props.userLeaderboardEnabled}`}</span>
      <button type='button' onClick={() => props.onPeriodChange('year')}>
        change period
      </button>
    </div>
  ),
}))

describe('rankings layout routing', () => {
  beforeEach(() => {
    mocks.navigate.mockReset()
    mocks.pathname = '/rankings/users'
  })

  test('marks the user tab active and renders the child route', () => {
    render(<RankingsLayout userLeaderboardEnabled />)

    expect(screen.getByText('view:users')).toBeInTheDocument()
    expect(screen.getByText('users:true')).toBeInTheDocument()
    expect(screen.getByText('active child route')).toBeInTheDocument()
  })

  test('period changes stay on the active child route', () => {
    render(<RankingsLayout userLeaderboardEnabled />)

    fireEvent.click(screen.getByRole('button', { name: 'change period' }))

    expect(mocks.navigate).toHaveBeenCalledWith({
      to: '/rankings/users',
      search: expect.any(Function),
    })
    const searchUpdater = mocks.navigate.mock.calls[0][0].search
    expect(searchUpdater({ period: 'month' })).toEqual({ period: 'year' })
  })
})

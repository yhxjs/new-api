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
import { render, screen, within } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import type { RankingPeriod, UserRanking } from '../../types'
import { UserRankingsSection } from '../user-rankings-section'

const USERS: UserRanking[] = [
  {
    rank: 1,
    username: 'alice',
    total_tokens: 1200,
    models: [
      {
        model_name: 'model-a',
        total_tokens: 1200,
        share: 1,
        is_other: false,
      },
    ],
  },
]

describe('user rankings section', () => {
  test('shows an isolated loading state', () => {
    render(
      <UserRankingsSection
        users={undefined}
        isLoading
        error={null}
        period='week'
      />
    )

    expect(
      screen.getByRole('status', { name: 'Loading user rankings' })
    ).toBeInTheDocument()
  })

  test('shows an isolated error state', () => {
    render(
      <UserRankingsSection
        users={undefined}
        isLoading={false}
        error={new Error('request failed')}
        period='week'
      />
    )

    expect(screen.getByText('Unable to load user rankings')).toBeInTheDocument()
  })

  test('shows the empty state when no users consumed tokens', () => {
    render(
      <UserRankingsSection
        users={[]}
        isLoading={false}
        error={null}
        period='week'
      />
    )

    expect(
      screen.getByText('No user ranking data available')
    ).toBeInTheDocument()
  })

  test('renders ranked users as one list with totals and model shares', () => {
    render(
      <UserRankingsSection
        users={USERS}
        isLoading={false}
        error={null}
        period='week'
      />
    )

    const section = screen.getByRole('region', { name: 'User Leaderboard' })
    expect(within(section).getAllByRole('listitem')).toHaveLength(1)
    expect(within(section).getByText('alice')).toBeInTheDocument()
    expect(within(section).getByText('1.2K')).toBeInTheDocument()
    expect(within(section).getByText('model-a')).toBeInTheDocument()
  })

  test.each<[RankingPeriod, string]>([
    ['today', 'Users ranked by total token usage across the last 24 hours'],
    ['week', 'Users ranked by total token usage across the past few weeks'],
    ['month', 'Users ranked by total token usage across the past month'],
    ['year', 'Users ranked by total token usage across the past year'],
  ])('describes the %s period range under the heading', (period, expected) => {
    render(
      <UserRankingsSection
        users={USERS}
        isLoading={false}
        error={null}
        period={period}
      />
    )

    const section = screen.getByRole('region', { name: 'User Leaderboard' })
    expect(within(section).getByText(expected)).toBeInTheDocument()
  })
})

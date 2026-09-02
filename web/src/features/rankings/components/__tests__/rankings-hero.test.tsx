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
import { describe, expect, test, vi } from 'vitest'

import { RankingsHero } from '../rankings-hero'

vi.mock('@tanstack/react-router', () => ({
  Link: (props: {
    'aria-selected'?: boolean
    children: React.ReactNode
    className?: string
    role?: string
    search: { period: string }
    to: string
  }) => (
    <a
      href={`${props.to}?period=${props.search.period}`}
      role={props.role}
      aria-selected={props['aria-selected']}
      className={props.className}
    >
      {props.children}
    </a>
  ),
}))

function renderHero(userLeaderboardEnabled = true) {
  render(
    <RankingsHero
      period='month'
      activeView='users'
      userLeaderboardEnabled={userLeaderboardEnabled}
      onPeriodChange={vi.fn()}
    />
  )
}

describe('rankings hero route tabs', () => {
  test('places the route tabs after the rankings description', () => {
    renderHero()

    const description = screen.getByText(
      'Discover the most-used models and rising vendors on the platform, updated from live usage data.'
    )
    const routeTabs = screen.getByRole('tablist', { name: 'Rankings' })

    expect(
      description.compareDocumentPosition(routeTabs) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
  })

  test('links each tab to its own route while preserving the period', () => {
    renderHero()

    const routeTabs = screen.getByRole('tablist', { name: 'Rankings' })
    expect(
      within(routeTabs).getByRole('tab', { name: 'Overall Leaderboard' })
    ).toHaveAttribute('href', '/rankings?period=month')
    expect(
      within(routeTabs).getByRole('tab', { name: 'User Leaderboard' })
    ).toHaveAttribute('href', '/rankings/users?period=month')
  })

  test('hides the user route tab while the feature is disabled', () => {
    renderHero(false)

    const routeTabs = screen.getByRole('tablist', { name: 'Rankings' })
    expect(
      within(routeTabs).queryByRole('tab', { name: 'User Leaderboard' })
    ).not.toBeInTheDocument()
  })
})

describe('rankings hero route tab indicator', () => {
  test('shows the indicator on the active route tab only', () => {
    renderHero()

    const routeTabs = screen.getByRole('tablist', { name: 'Rankings' })
    const activeTab = within(routeTabs).getByRole('tab', {
      name: 'User Leaderboard',
    })
    const inactiveTab = within(routeTabs).getByRole('tab', {
      name: 'Overall Leaderboard',
    })

    expect(activeTab.querySelector('span[aria-hidden]')).toHaveClass(
      'opacity-100'
    )
    expect(inactiveTab.querySelector('span[aria-hidden]')).toHaveClass(
      'opacity-0'
    )
  })

  test('anchors the indicator inside the scrollable tablist so it is not clipped', () => {
    renderHero()

    const routeTabs = screen.getByRole('tablist', { name: 'Rankings' })
    const activeTab = within(routeTabs).getByRole('tab', {
      name: 'User Leaderboard',
    })

    // `overflow-x-auto` makes the tablist clip vertically too, so the
    // indicator and the tab must stay within its padding box and the bottom
    // border must live on the non-scrolling wrapper.
    expect(routeTabs).toHaveClass('overflow-x-auto')
    expect(routeTabs.className).not.toContain('border-b')
    expect(routeTabs.parentElement).toHaveClass('border-b')
    expect(activeTab.className).not.toContain('-mb-px')
    expect(activeTab.querySelector('span[aria-hidden]')).toHaveClass('bottom-0')
  })
})

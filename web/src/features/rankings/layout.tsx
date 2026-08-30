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
import {
  Outlet,
  useNavigate,
  useRouterState,
  useSearch,
} from '@tanstack/react-router'

import { PublicLayout } from '@/components/layout'
import { PageTransition } from '@/components/page-transition'

import { RankingsHero } from './components'
import type { RankingPeriod } from './types'

type RankingsLayoutProps = {
  userLeaderboardEnabled: boolean
}

export function RankingsLayout(props: RankingsLayoutProps) {
  const search = useSearch({ from: '/rankings' })
  const navigate = useNavigate()
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  })
  const activeView = pathname === '/rankings/users' ? 'users' : 'overall'
  const period: RankingPeriod = search.period ?? 'week'

  const handlePeriodChange = (next: RankingPeriod) => {
    navigate({
      to: activeView === 'users' ? '/rankings/users' : '/rankings',
      search: (previous) => ({ ...previous, period: next }),
    })
  }

  return (
    <PublicLayout showMainContainer={false}>
      <div className='relative'>
        <div
          aria-hidden
          className='pointer-events-none absolute inset-x-0 top-0 h-[600px] opacity-20 dark:opacity-[0.10]'
          style={{
            background: [
              'radial-gradient(ellipse 60% 50% at 20% 20%, oklch(0.72 0.18 250 / 80%) 0%, transparent 70%)',
              'radial-gradient(ellipse 50% 40% at 80% 15%, oklch(0.65 0.15 200 / 60%) 0%, transparent 70%)',
              'radial-gradient(ellipse 40% 35% at 50% 70%, oklch(0.70 0.12 280 / 40%) 0%, transparent 70%)',
            ].join(', '),
            maskImage:
              'linear-gradient(to bottom, black 40%, transparent 100%)',
            WebkitMaskImage:
              'linear-gradient(to bottom, black 40%, transparent 100%)',
          }}
        />
        <PageTransition className='relative mx-auto w-full max-w-[1280px] space-y-8 px-3 pt-16 pb-10 sm:px-6 sm:pt-20 sm:pb-12 xl:px-8'>
          <RankingsHero
            period={period}
            activeView={activeView}
            userLeaderboardEnabled={props.userLeaderboardEnabled}
            onPeriodChange={handlePeriodChange}
          />

          <Outlet />
        </PageTransition>
      </div>
    </PublicLayout>
  )
}

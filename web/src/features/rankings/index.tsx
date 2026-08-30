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
import { useSearch } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'

import { MarketShareSection, ModelsSection, PulseSection } from './components'
import { useRankings } from './hooks/use-rankings'
import type { RankingPeriod } from './types'

export function Rankings() {
  const { t } = useTranslation()
  const search = useSearch({ from: '/rankings' })
  const period: RankingPeriod = search.period ?? 'week'

  const rankingsQuery = useRankings(period)
  const snapshot = rankingsQuery.data?.data

  let rankingsContent = <RankingsLoading />
  if (!rankingsQuery.isLoading) {
    if (!snapshot) {
      rankingsContent = (
        <RankingsError
          message={
            rankingsQuery.error instanceof Error
              ? rankingsQuery.error.message
              : t('Unable to load rankings data')
          }
        />
      )
    } else {
      rankingsContent = (
        <>
          <ModelsSection
            history={snapshot.models_history}
            rows={snapshot.models}
            period={period}
          />

          <MarketShareSection
            history={snapshot.vendor_share_history}
            rows={snapshot.vendors}
            period={period}
          />

          <PulseSection
            movers={snapshot.top_movers}
            droppers={snapshot.top_droppers}
          />
        </>
      )
    }
  }

  return rankingsContent
}

function RankingsLoading() {
  return (
    <div className='space-y-6'>
      <Skeleton className='h-[420px] w-full rounded-xl' />
      <Skeleton className='h-[360px] w-full rounded-xl' />
      <Skeleton className='h-[180px] w-full rounded-xl' />
    </div>
  )
}

function RankingsError(props: { message: string }) {
  const { t } = useTranslation()
  return (
    <div className='bg-card rounded-xl border border-dashed px-6 py-12 text-center'>
      <h2 className='text-foreground text-base font-semibold'>
        {t('Unable to load rankings')}
      </h2>
      <p className='text-muted-foreground mx-auto mt-2 max-w-md text-sm'>
        {props.message}
      </p>
    </div>
  )
}

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
import { Users } from 'lucide-react'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Skeleton } from '@/components/ui/skeleton'

import { formatTokens } from '../lib/format'
import type { UserRanking } from '../types'
import { UserModelShareBar } from './user-model-share-bar'

type UserRankingsSectionProps = {
  users: UserRanking[] | undefined
  isLoading: boolean
  error: Error | null
}

export function UserRankingsSection(props: UserRankingsSectionProps) {
  const { t } = useTranslation()
  const titleId = useId()

  return (
    <section
      role='region'
      aria-labelledby={titleId}
      className='bg-card overflow-hidden rounded-lg border'
    >
      <header className='px-5 py-4'>
        <h2
          id={titleId}
          className='text-foreground inline-flex items-center gap-2 text-base font-semibold'
        >
          <Users className='text-primary size-4' aria-hidden='true' />
          {t('User Leaderboard')}
        </h2>
        <p className='text-muted-foreground mt-1 text-sm'>
          {t('Users ranked by total token usage')}
        </p>
      </header>

      <UserRankingsContent {...props} />
    </section>
  )
}

function UserRankingsContent(props: UserRankingsSectionProps) {
  const { t } = useTranslation()

  if (props.isLoading) {
    return (
      <div
        role='status'
        aria-label={t('Loading user rankings')}
        className='space-y-5 border-t px-5 py-5'
      >
        {[0, 1, 2].map((index) => (
          <div key={index} aria-hidden='true' className='space-y-3'>
            <div className='flex items-center gap-3'>
              <Skeleton className='size-8 rounded-full' />
              <Skeleton className='h-4 w-36' />
              <Skeleton className='ml-auto h-4 w-20' />
            </div>
            <Skeleton className='h-2 w-full rounded-sm' />
            <Skeleton className='h-4 w-3/4' />
          </div>
        ))}
      </div>
    )
  }

  if (props.error) {
    return (
      <div className='text-muted-foreground border-t px-5 py-10 text-center text-sm'>
        {t('Unable to load user rankings')}
      </div>
    )
  }

  if (!props.users || props.users.length === 0) {
    return (
      <div className='text-muted-foreground border-t px-5 py-10 text-center text-sm'>
        {t('No user ranking data available')}
      </div>
    )
  }

  return (
    <ol className='divide-y border-t'>
      {props.users.map((user) => (
        <UserRankingRow key={`${user.rank}-${user.username}`} user={user} />
      ))}
    </ol>
  )
}

function UserRankingRow(props: { user: UserRanking }) {
  const { t } = useTranslation()
  const avatarFallback = [...props.user.username.trim()][0] ?? '?'

  return (
    <li className='px-5 py-4'>
      <div className='mb-3 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2'>
        <span className='text-muted-foreground/80 w-6 shrink-0 text-right font-mono text-xs tabular-nums'>
          {props.user.rank}.
        </span>
        <Avatar size='sm'>
          <AvatarFallback className='font-mono uppercase'>
            {avatarFallback}
          </AvatarFallback>
        </Avatar>
        <span className='text-foreground min-w-0 flex-1 truncate text-sm font-medium'>
          {props.user.username}
        </span>
        <div className='ml-9 flex shrink-0 items-baseline gap-1 sm:ml-0'>
          <span className='text-foreground font-mono text-sm font-semibold tabular-nums'>
            {formatTokens(props.user.total_tokens)}
          </span>
          <span className='text-muted-foreground text-xs'>{t('tokens')}</span>
        </div>
      </div>
      <div className='ml-9 sm:ml-12'>
        <UserModelShareBar models={props.user.models} />
      </div>
    </li>
  )
}

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
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  DatabaseZap,
  RefreshCw,
  Search,
  ServerOff,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { PageTransition } from '@/components/page-transition'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'

import { getModelStatus } from './api'
import { ModelStatusCard } from './components/model-status-card'
import { StatusSummary } from './components/status-summary'
import type { ModelStatusModel, ModelStatusSort } from './types'

export function ModelStatusPage() {
  const { t } = useTranslation()
  const [selectedGroup, setSelectedGroup] = useState('all')
  const [search, setSearch] = useState('')
  const [sortBy, setSortBy] = useState<ModelStatusSort>('request-count')
  const statusQuery = useQuery({
    queryKey: ['model-status', selectedGroup],
    queryFn: () => getModelStatus(selectedGroup),
    staleTime: 60_000,
    retry: false,
  })
  const snapshot = statusQuery.data?.data

  const models = useMemo(() => {
    const normalizedSearch = search.trim().toLocaleLowerCase()
    const filtered = (snapshot?.models ?? []).filter((model) =>
      model.model_name.toLocaleLowerCase().includes(normalizedSearch)
    )

    return [...filtered].sort((left, right) => {
      let comparison = 0
      if (sortBy === 'request-count') {
        comparison = right.request_count - left.request_count
      } else if (sortBy === 'success-rate') {
        comparison = left.success_rate - right.success_rate
      } else {
        comparison = compareFirstTokenLatency(left, right)
      }
      return comparison || left.model_name.localeCompare(right.model_name)
    })
  }, [search, snapshot?.models, sortBy])

  const updatedAt = snapshot
    ? new Intl.DateTimeFormat(undefined, {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      }).format(new Date(snapshot.updated_at * 1000))
    : null

  return (
    <PublicLayout showMainContainer={false}>
      <PageTransition className='mx-auto w-full max-w-[1280px] space-y-6 px-4 pt-20 pb-10 sm:px-6 sm:pt-24 lg:px-8'>
        <header className='flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between'>
          <div className='min-w-0'>
            <div className='flex items-center gap-2'>
              <Activity className='text-primary size-5' aria-hidden='true' />
              <h1 className='text-foreground text-2xl font-semibold text-balance'>
                {t('Model Status')}
              </h1>
            </div>
            <p className='text-muted-foreground mt-2 max-w-2xl text-sm text-pretty'>
              {t('Availability and performance from the last 24 hours.')}
            </p>
            {updatedAt ? (
              <p className='text-muted-foreground mt-1 text-xs'>
                {t('Updated {{time}}', { time: updatedAt })}
              </p>
            ) : null}
          </div>

          <div className='flex w-full flex-col gap-2 sm:flex-row lg:w-auto'>
            <NativeSelect
              aria-label={t('Model group')}
              value={selectedGroup}
              onChange={(event) => setSelectedGroup(event.target.value)}
              className='w-full sm:w-44'
            >
              <NativeSelectOption value='all'>
                {t('All groups')}
              </NativeSelectOption>
              {(snapshot?.groups ?? []).map((group) => (
                <NativeSelectOption key={group} value={group}>
                  {group}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <NativeSelect
              aria-label={t('Sort models')}
              value={sortBy}
              onChange={(event) =>
                setSortBy(event.target.value as ModelStatusSort)
              }
              className='w-full sm:w-48'
            >
              <NativeSelectOption value='request-count'>
                {t('Request count')} ↓
              </NativeSelectOption>
              <NativeSelectOption value='success-rate'>
                {t('Success rate')} ↑
              </NativeSelectOption>
              <NativeSelectOption value='first-token-latency'>
                {t('First token latency')} ↓
              </NativeSelectOption>
            </NativeSelect>
          </div>
        </header>

        {snapshot ? <StatusSummary summary={snapshot.summary} /> : null}

        <section aria-label={t('Model status results')} className='space-y-4'>
          <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
            <InputGroup className='w-full sm:max-w-sm'>
              <InputGroupAddon>
                <Search aria-hidden='true' />
              </InputGroupAddon>
              <InputGroupInput
                type='search'
                aria-label={t('Search models')}
                placeholder={t('Search models')}
                value={search}
                onChange={(event) => setSearch(event.target.value)}
              />
            </InputGroup>
            {snapshot ? (
              <p className='text-muted-foreground text-xs tabular-nums'>
                {t('Showing {{count}} of {{total}} models', {
                  count: models.length,
                  total: snapshot.models.length,
                })}
              </p>
            ) : null}
          </div>

          <ModelStatusContent
            isPending={statusQuery.isPending}
            isError={statusQuery.isError}
            collectionEnabled={snapshot?.collection_enabled ?? true}
            totalModels={snapshot?.models.length ?? 0}
            visibleModels={models}
            onRetry={() => void statusQuery.refetch()}
          />
        </section>
      </PageTransition>
    </PublicLayout>
  )
}

function compareFirstTokenLatency(
  left: ModelStatusModel,
  right: ModelStatusModel
): number {
  if (left.avg_ttft_ms === null) return right.avg_ttft_ms === null ? 0 : 1
  if (right.avg_ttft_ms === null) return -1
  return right.avg_ttft_ms - left.avg_ttft_ms
}

type ModelStatusContentProps = {
  isPending: boolean
  isError: boolean
  collectionEnabled: boolean
  totalModels: number
  visibleModels: ModelStatusModel[]
  onRetry: () => void
}

function ModelStatusContent(props: ModelStatusContentProps) {
  const { t } = useTranslation()

  if (props.isPending) {
    return (
      <div className='grid gap-4 lg:grid-cols-2' aria-label={t('Loading')}>
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className='h-64 rounded-lg' />
        ))}
      </div>
    )
  }

  if (props.isError) {
    return (
      <StatusEmptyState
        icon={ServerOff}
        title={t('Unable to load model status')}
        description={t('The latest status snapshot could not be loaded.')}
      >
        <Button variant='outline' onClick={props.onRetry}>
          <RefreshCw aria-hidden='true' />
          {t('Try again')}
        </Button>
      </StatusEmptyState>
    )
  }

  if (!props.collectionEnabled) {
    return (
      <StatusEmptyState
        icon={DatabaseZap}
        title={t('Performance metrics collection is disabled')}
        description={t('No model health data is being collected right now.')}
      />
    )
  }

  if (props.totalModels === 0) {
    return (
      <StatusEmptyState
        icon={Activity}
        title={t('No model activity in the last 24 hours')}
        description={t('Status data will appear after models receive traffic.')}
      />
    )
  }

  if (props.visibleModels.length === 0) {
    return (
      <StatusEmptyState
        icon={Search}
        title={t('No models match your search')}
        description={t('Try a different model name.')}
      />
    )
  }

  return (
    <div className='grid items-start gap-4 lg:grid-cols-2'>
      {props.visibleModels.map((model) => (
        <ModelStatusCard key={model.model_name} model={model} />
      ))}
    </div>
  )
}

type StatusEmptyStateProps = {
  icon: React.ComponentType<{ className?: string; 'aria-hidden'?: boolean }>
  title: string
  description: string
  children?: React.ReactNode
}

function StatusEmptyState(props: StatusEmptyStateProps) {
  const Icon = props.icon
  return (
    <Empty className='bg-card min-h-64 border'>
      <EmptyHeader>
        <EmptyMedia variant='icon'>
          <Icon aria-hidden />
        </EmptyMedia>
        <EmptyTitle>{props.title}</EmptyTitle>
        <EmptyDescription>{props.description}</EmptyDescription>
      </EmptyHeader>
      {props.children ? <EmptyContent>{props.children}</EmptyContent> : null}
    </Empty>
  )
}

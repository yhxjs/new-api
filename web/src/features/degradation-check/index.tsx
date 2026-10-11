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
import { Link } from '@tanstack/react-router'
import {
  Activity,
  Play,
  RefreshCw,
  Search,
  Settings,
  ShieldCheck,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { PublicLayout } from '@/components/layout'
import { PageTransition } from '@/components/page-transition'
import { Alert, AlertAction, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Empty,
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
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { getDegradationOverview, triggerDegradationRun } from './api'
import {
  ChannelModelCard,
  type ModelChannelInstance,
} from './components/channel-model-card'
import { StatusSummary } from './components/status-summary'
import { DEGRADED_PASS_RATE, HEALTHY_PASS_RATE } from './constants'
import { localizeDegradationMessage } from './lib/degradation-messages'
import type {
  DegradationModelItem,
  DegradationSortOption,
  DegradationStatusFilter,
} from './types'

export function DegradationCheckPage() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const isAdmin = user ? user.role >= ROLE.ADMIN : false
  const canConfigure = user?.role === ROLE.SUPER_ADMIN
  const canReadLogs = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.READ
  )
  const canOperate = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.OPERATE
  )

  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] =
    useState<DegradationStatusFilter>('all')
  const [sortBy, setSortBy] = useState<DegradationSortOption>('worst-pass-rate')
  const [triggering, setTriggering] = useState(false)

  const overviewQuery = useQuery({
    queryKey: ['degradation-overview'],
    queryFn: getDegradationOverview,
    refetchInterval: 30_000,
    retry: 1,
  })

  const overview = overviewQuery.data?.data

  // 将按渠道分组的数据转换为按模型分组（便于横向对比多渠道表现）
  const modelGroups = useMemo(() => {
    if (!overview?.channels) return []

    const map = new Map<string, ModelChannelInstance[]>()

    for (const ch of overview.channels) {
      for (const m of ch.models) {
        if (!m.enabled) continue
        const list = map.get(m.model_name) || []
        list.push({
          channelId: ch.channel_id ?? 0,
          channelName: ch.channel_name,
          model: m,
        })
        map.set(m.model_name, list)
      }
    }

    const groups: {
      modelName: string
      instances: ModelChannelInstance[]
      avgPassRate: number | null
      avgLatency: number
    }[] = []

    map.forEach((instances, modelName) => {
      // Weight by execution counts when available so a busy channel's result
      // is not diluted by a node that was only probed once.
      const totalExec = instances.reduce(
        (acc, inst) => acc + (inst.model.total_executions ?? 0),
        0
      )
      const weightedAvg = (pick: (m: DegradationModelItem) => number) =>
        totalExec > 0
          ? instances.reduce(
              (acc, inst) =>
                acc + pick(inst.model) * (inst.model.total_executions ?? 0),
              0
            ) / totalExec
          : 0

      groups.push({
        modelName,
        instances,
        avgPassRate: totalExec > 0 ? weightedAvg((m) => m.pass_rate) : null,
        avgLatency: weightedAvg((m) => m.avg_latency_ms),
      })
    })

    // 过滤
    const term = search.trim().toLowerCase()
    const filtered = groups.filter((g) => {
      // 文本搜索
      const matchText =
        !term ||
        g.modelName.toLowerCase().includes(term) ||
        g.instances.some((inst) =>
          inst.channelName.toLowerCase().includes(term)
        )
      if (!matchText) return false

      // 状态筛选
      if (g.avgPassRate === null) return statusFilter === 'all'
      if (statusFilter === 'normal') {
        return g.avgPassRate >= HEALTHY_PASS_RATE
      }
      if (statusFilter === 'degraded') {
        return (
          g.avgPassRate >= DEGRADED_PASS_RATE &&
          g.avgPassRate < HEALTHY_PASS_RATE
        )
      }
      if (statusFilter === 'error') {
        return g.avgPassRate < DEGRADED_PASS_RATE
      }
      return true
    })

    // 排序
    return filtered.sort((a, b) => {
      if (sortBy === 'worst-pass-rate') {
        return (a.avgPassRate ?? Infinity) - (b.avgPassRate ?? Infinity)
      }
      if (sortBy === 'highest-latency') {
        return b.avgLatency - a.avgLatency
      }
      if (sortBy === 'most-models') {
        return b.instances.length - a.instances.length
      }
      return a.modelName.localeCompare(b.modelName)
    })
  }, [overview?.channels, search, sortBy, statusFilter])

  const handleTriggerRun = async () => {
    try {
      setTriggering(true)
      const res = await triggerDegradationRun()
      if (res.success) {
        toast.success(t('Degradation test triggered'))
        overviewQuery.refetch()
      } else {
        toast.error(
          localizeDegradationMessage(res.message, t('Failed to trigger test'))
        )
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err)
      toast.error(localizeDegradationMessage(msg, t('Failed to trigger test')))
    } finally {
      setTriggering(false)
    }
  }

  return (
    <PublicLayout showMainContainer={false}>
      <PageTransition className='mx-auto w-full max-w-[1280px] space-y-6 px-4 pt-20 pb-12 sm:px-6 sm:pt-24 lg:px-8'>
        {/* 页头 */}
        <header className='flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between'>
          <div className='min-w-0'>
            <div className='flex items-center gap-2'>
              <h1 className='text-foreground text-2xl font-bold tracking-tight sm:text-3xl'>
                {t('Degradation Check')}
              </h1>
              <span className='inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2.5 py-0.5 text-xs font-medium text-emerald-600 dark:text-emerald-400'>
                <ShieldCheck className='h-3.5 w-3.5' />
                {t('Live Probing')}
              </span>
            </div>
            <p className='text-muted-foreground mt-1.5 text-xs sm:text-sm'>
              {t(
                'Continuous semantic logic testing, reasoning capability evaluation, and output degeneration detection across all model nodes.'
              )}
            </p>
          </div>

          <div className='flex flex-wrap items-center gap-2'>
            {canOperate && (
              <Button
                size='sm'
                variant='outline'
                disabled={triggering}
                onClick={handleTriggerRun}
              >
                <Play className='mr-1.5 h-3.5 w-3.5' />
                {triggering ? t('Triggering...') : t('Run Check Now')}
              </Button>
            )}

            {canConfigure && (
              <Link
                to='/system-settings/models/$section'
                params={{ section: 'degradation-check' }}
                className='border-input bg-background hover:bg-accent hover:text-accent-foreground inline-flex h-8 items-center rounded-md border px-3 text-xs font-medium shadow-xs transition'
              >
                <Settings className='mr-1.5 h-3.5 w-3.5' />
                {t('Configure Probes')}
              </Link>
            )}

            <Button
              size='icon-sm'
              variant='outline'
              onClick={() => overviewQuery.refetch()}
              disabled={overviewQuery.isFetching}
              aria-label={t('Refresh')}
            >
              <RefreshCw
                className={`h-3.5 w-3.5 ${
                  overviewQuery.isFetching ? 'animate-spin' : ''
                }`}
              />
            </Button>
          </div>
        </header>

        {overviewQuery.isError && (
          <Alert variant='destructive'>
            <AlertTitle>
              {overview
                ? t(
                    'Failed to refresh degradation data. Showing the last successful results.'
                  )
                : t('Failed to load degradation data.')}
            </AlertTitle>
            <AlertAction>
              <Button
                size='sm'
                variant='outline'
                onClick={() => overviewQuery.refetch()}
                disabled={overviewQuery.isFetching}
              >
                {t('Retry')}
              </Button>
            </AlertAction>
          </Alert>
        )}

        {/* 顶部指标卡片 */}
        {(!overviewQuery.isError || overview) && (
          <StatusSummary stats={overview?.stats} />
        )}

        {/* 搜索与过滤控制条 */}
        <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
          <div className='max-w-sm flex-1'>
            <InputGroup className='h-9'>
              <InputGroupAddon>
                <Search className='text-muted-foreground h-4 w-4' />
              </InputGroupAddon>
              <InputGroupInput
                placeholder={t('Search model or node name...')}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className='text-xs'
              />
            </InputGroup>
          </div>

          <div className='flex flex-wrap items-center gap-2.5'>
            {/* 状态筛选 */}
            <div className='bg-muted/30 flex items-center gap-1 rounded-lg border p-1 text-xs'>
              {(
                [
                  { id: 'all', label: t('All') },
                  { id: 'normal', label: t('Normal') },
                  { id: 'degraded', label: t('Degraded') },
                  { id: 'error', label: t('Error') },
                ] as const
              ).map((tab) => (
                <button
                  key={tab.id}
                  type='button'
                  onClick={() => setStatusFilter(tab.id)}
                  className={`rounded-md px-2.5 py-1 font-medium transition ${
                    statusFilter === tab.id
                      ? 'bg-background text-foreground shadow-xs'
                      : 'text-muted-foreground hover:text-foreground'
                  }`}
                >
                  {tab.label}
                </button>
              ))}
            </div>

            {/* 排序 */}
            <NativeSelect
              value={sortBy}
              onChange={(e) =>
                setSortBy(e.target.value as DegradationSortOption)
              }
              className='h-8 text-xs'
            >
              <NativeSelectOption value='worst-pass-rate'>
                {t('Worst Pass Rate First')}
              </NativeSelectOption>
              <NativeSelectOption value='highest-latency'>
                {t('Highest Latency First')}
              </NativeSelectOption>
              <NativeSelectOption value='most-models'>
                {t('Most Nodes First')}
              </NativeSelectOption>
              <NativeSelectOption value='alphabetical'>
                {t('Alphabetical')}
              </NativeSelectOption>
            </NativeSelect>
          </div>
        </div>

        {/* 模型列表 / 骨架屏 / 空状态 */}
        {overviewQuery.isLoading && (
          <div className='space-y-4'>
            <Skeleton className='h-32 w-full rounded-2xl' />
            <Skeleton className='h-32 w-full rounded-2xl' />
            <Skeleton className='h-32 w-full rounded-2xl' />
          </div>
        )}

        {!overviewQuery.isLoading &&
          !overviewQuery.isError &&
          modelGroups.length === 0 && (
            <Empty className='bg-card/60 rounded-2xl border p-12 text-center'>
              <EmptyMedia>
                <Activity className='text-muted-foreground/60 h-10 w-10' />
              </EmptyMedia>
              <EmptyHeader>
                <EmptyTitle>{t('No Model Degradation Data Found')}</EmptyTitle>
                <EmptyDescription>
                  {t(
                    'No models currently match your filter or no degradation checks have been scheduled yet.'
                  )}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}

        {!overviewQuery.isLoading && modelGroups.length > 0 && (
          <div className='space-y-4'>
            {modelGroups.map((group) => (
              <ChannelModelCard
                key={group.modelName}
                modelName={group.modelName}
                instances={group.instances}
                isAdmin={isAdmin}
                canReadLogs={canReadLogs}
                canOperate={canOperate}
              />
            ))}
          </div>
        )}
      </PageTransition>
    </PublicLayout>
  )
}

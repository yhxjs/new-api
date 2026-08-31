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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import {
  formatThroughput,
  getSuccessRateLevel,
  getSuccessRateTextClass,
} from '@/features/performance-metrics/lib/format'
import { formatNumber, formatPercent } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { ModelStatusModel } from '../types'
import { StatusTimeline } from './status-timeline'

type ModelStatusCardProps = {
  model: ModelStatusModel
}

function formatFirstTokenLatency(value: number | null): string {
  if (value === null || !Number.isFinite(value)) return '—'
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}s`
  return `${Math.round(value)}ms`
}

function formatModelThroughput(value: number | null): string {
  if (value === null || !Number.isFinite(value)) return '—'
  if (value === 0) return '0 t/s'
  return formatThroughput(value)
}

export function ModelStatusCard(props: ModelStatusCardProps) {
  const { t } = useTranslation()
  const level = getSuccessRateLevel(props.model.success_rate)
  const status = level === 'excellent' || level === 'good' ? 'healthy' : level
  const statusConfig = {
    healthy: {
      label: t('Healthy'),
      className: 'border-success/30 bg-success/10 text-success',
    },
    warning: {
      label: t('Warning'),
      className: 'border-warning/30 bg-warning/10 text-warning',
    },
    critical: {
      label: t('Critical'),
      className: 'border-destructive/30 bg-destructive/10 text-destructive',
    },
    unknown: {
      label: t('Unknown'),
      className: 'border-border bg-muted text-muted-foreground',
    },
  }[status]

  const requestMetrics = [
    { label: t('Requests'), value: props.model.request_count },
    { label: t('Successful'), value: props.model.success_count },
    { label: t('Failed'), value: props.model.failure_count },
  ]

  return (
    <article
      aria-label={props.model.model_name}
      className='bg-card min-w-0 rounded-lg border p-4 sm:p-5'
    >
      <header className='flex min-w-0 items-start justify-between gap-3'>
        <div className='min-w-0'>
          <h2
            title={props.model.model_name}
            className='text-foreground text-sm font-semibold break-all'
          >
            {props.model.model_name}
          </h2>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('Last 24 hours')}
          </p>
        </div>
        <Badge variant='outline' className={statusConfig.className}>
          <span
            aria-hidden='true'
            className='size-1.5 rounded-full bg-current'
          />
          {statusConfig.label}
        </Badge>
      </header>

      <div className='mt-4 grid grid-cols-[minmax(0,1.15fr)_repeat(3,minmax(0,0.8fr))] gap-3 border-y py-3'>
        <div className='min-w-0'>
          <div
            className={cn(
              'font-mono text-xl font-semibold tabular-nums',
              getSuccessRateTextClass(props.model.success_rate)
            )}
          >
            {formatPercent(props.model.success_rate)}
          </div>
          <div className='text-muted-foreground mt-0.5 text-[11px] text-pretty'>
            {t('Success rate')}
          </div>
        </div>
        {requestMetrics.map((metric) => (
          <div key={metric.label} className='min-w-0'>
            <div className='text-foreground font-mono text-sm font-semibold tabular-nums'>
              {formatNumber(metric.value)}
            </div>
            <div className='text-muted-foreground mt-1 text-[11px] text-pretty'>
              {metric.label}
            </div>
          </div>
        ))}
      </div>

      <div className='mt-4'>
        <div className='text-muted-foreground mb-2 flex items-center justify-between text-[11px]'>
          <span>{t('Hourly success rate')}</span>
          <span>{t('Older to newer')}</span>
        </div>
        <StatusTimeline buckets={props.model.buckets} />
      </div>

      <dl className='mt-4 grid grid-cols-2 gap-4 border-t pt-3'>
        <div className='min-w-0'>
          <dt className='text-muted-foreground min-h-8 text-[11px] leading-4 text-pretty sm:min-h-0'>
            {t('Average first token')}
          </dt>
          <dd className='text-foreground mt-1 font-mono text-sm font-medium tabular-nums'>
            {formatFirstTokenLatency(props.model.avg_ttft_ms)}
          </dd>
        </div>
        <div className='min-w-0'>
          <dt className='text-muted-foreground min-h-8 text-[11px] leading-4 text-pretty sm:min-h-0'>
            {t('Average output speed')}
          </dt>
          <dd className='text-foreground mt-1 font-mono text-sm font-medium tabular-nums'>
            {formatModelThroughput(props.model.avg_tps)}
          </dd>
        </div>
      </dl>
    </article>
  )
}

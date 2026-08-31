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
import { Activity, Gauge, Server, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { formatNumber, formatPercent } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { ModelStatusSummary as ModelStatusSummaryData } from '../types'

type StatusSummaryProps = {
  summary: ModelStatusSummaryData
}

export function StatusSummary(props: StatusSummaryProps) {
  const { t } = useTranslation()
  const items = [
    {
      label: t('Active models'),
      value: formatNumber(props.summary.model_count),
      detail: t('With requests in this window'),
      icon: Server,
    },
    {
      label: t('Total requests'),
      value: formatNumber(props.summary.request_count),
      detail: t('{{count}} successful', {
        count: formatNumber(props.summary.success_count),
      }),
      icon: Activity,
    },
    {
      label: t('Overall success rate'),
      value: formatPercent(props.summary.success_rate),
      detail: t('Weighted by request volume'),
      icon: Gauge,
    },
    {
      label: t('Needs attention'),
      value: formatNumber(props.summary.attention_count),
      detail: t('{{count}} critical', {
        count: formatNumber(props.summary.critical_count),
      }),
      icon: TriangleAlert,
    },
  ]

  return (
    <section
      aria-label={t('Status summary')}
      className='border-border grid grid-cols-2 gap-y-5 border-y py-4 lg:grid-cols-4 lg:gap-y-0'
    >
      {items.map((item, index) => {
        const Icon = item.icon
        return (
          <div
            key={item.label}
            className={cn(
              'min-w-0 px-1 sm:px-3 lg:border-l lg:px-5',
              index === 0 ? 'lg:border-l-0 lg:pl-1' : ''
            )}
          >
            <div className='text-muted-foreground flex items-start gap-2 text-xs font-medium'>
              <Icon className='mt-0.5 size-3.5 shrink-0' aria-hidden='true' />
              <span className='min-h-8 leading-4 text-pretty sm:min-h-0'>
                {item.label}
              </span>
            </div>
            <div className='text-foreground mt-1.5 font-mono text-2xl font-semibold tabular-nums'>
              {item.value}
            </div>
            <p className='text-muted-foreground mt-0.5 min-h-8 text-xs leading-4 text-pretty sm:min-h-0'>
              {item.detail}
            </p>
          </div>
        )
      })}
    </section>
  )
}

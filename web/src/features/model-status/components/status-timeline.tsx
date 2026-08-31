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

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getSuccessRateLevel } from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

import type { ModelStatusBucket } from '../types'

type StatusTimelineProps = {
  buckets: ModelStatusBucket[]
}

const STATUS_CLASS = {
  healthy: 'bg-success hover:bg-success/80',
  warning: 'bg-warning hover:bg-warning/80',
  critical: 'bg-destructive hover:bg-destructive/80',
  unknown: 'border border-border bg-muted/40 hover:bg-muted',
} as const

type TimelineStatus = keyof typeof STATUS_CLASS

function getTimelineStatus(bucket: ModelStatusBucket): TimelineStatus {
  if (!bucket.has_data) return 'unknown'
  const level = getSuccessRateLevel(bucket.success_rate)
  if (level === 'excellent' || level === 'good') return 'healthy'
  return level === 'warning' ? 'warning' : 'critical'
}

export function StatusTimeline(props: StatusTimelineProps) {
  const { t } = useTranslation()
  const dateFormatter = new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
  const numberFormatter = new Intl.NumberFormat()

  return (
    <TooltipProvider delay={150}>
      <div
        aria-label={t('Hourly success rate')}
        className='grid h-5 w-full grid-cols-[repeat(24,minmax(0,1fr))] gap-0.5'
      >
        {props.buckets.map((bucket) => {
          const status = getTimelineStatus(bucket)
          const time = dateFormatter.format(new Date(bucket.ts * 1000))
          const rate = `${bucket.success_rate.toFixed(2)}%`
          const label = bucket.has_data
            ? t('{{time}}: {{requests}} requests, {{rate}} success rate', {
                time,
                requests: numberFormatter.format(bucket.request_count),
                rate,
              })
            : t('{{time}}: No data', { time })

          return (
            <Tooltip key={bucket.ts}>
              <TooltipTrigger
                render={
                  <button
                    type='button'
                    aria-label={label}
                    data-status={status}
                    className={cn(
                      'focus-visible:ring-ring min-w-0 rounded-[2px] outline-none transition-colors focus-visible:ring-2 focus-visible:ring-offset-2',
                      STATUS_CLASS[status]
                    )}
                  />
                }
              />
              <TooltipContent>
                <span className='font-medium'>{time}</span>
                <span aria-hidden>·</span>
                <span>
                  {bucket.has_data
                    ? t('{{requests}} requests at {{rate}}', {
                        requests: numberFormatter.format(bucket.request_count),
                        rate,
                      })
                    : t('No data')}
                </span>
              </TooltipContent>
            </Tooltip>
          )
        })}
      </div>
    </TooltipProvider>
  )
}

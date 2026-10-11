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

type TimelineStripProps = {
  slots?: number[] // 24 slots (index 0 is 24h ago, index 23 is now)
  probeName?: string
}

export function TimelineStrip(props: TimelineStripProps) {
  const { t } = useTranslation()
  const slots = props.slots || Array(24).fill(0)

  const getSlotColor = (status: number) => {
    switch (status) {
      case 1:
        return 'bg-emerald-500 hover:bg-emerald-400'
      case 2:
        return 'bg-amber-500 hover:bg-amber-400 animate-pulse'
      case 3:
      case 4:
        return 'bg-rose-500 hover:bg-rose-400'
      default:
        return 'bg-muted/70 hover:bg-muted'
    }
  }

  const getSlotDesc = (status: number, hoursAgo: number) => {
    const timeLabel =
      hoursAgo === 0
        ? t('Current Hour')
        : t('{{count}}h ago', { count: hoursAgo })
    let statusLabel = t('No Data')
    if (status === 1) statusLabel = t('Passed (100%)')
    if (status === 2) statusLabel = t('Mismatched / Degraded')
    if (status === 3) statusLabel = t('Execution Error')
    if (status === 4) statusLabel = t('Request Timeout')

    return `${timeLabel}: ${statusLabel}`
  }

  return (
    <TooltipProvider delay={100}>
      <div className='flex w-full items-center gap-1 sm:gap-1.5'>
        {slots.map((status, idx) => {
          const hoursAgo = 23 - idx
          const slotDescription = getSlotDesc(status, hoursAgo)
          return (
            <Tooltip key={`slot-${hoursAgo}`}>
              <TooltipTrigger
                aria-label={
                  props.probeName
                    ? `${props.probeName}: ${slotDescription}`
                    : slotDescription
                }
                className={`h-4.5 flex-1 cursor-pointer rounded-[3px] transition ${getSlotColor(
                  status
                )}`}
              />
              <TooltipContent side='top' className='font-mono text-[11px]'>
                <div className='space-y-0.5'>
                  {props.probeName && (
                    <div className='text-[10px] font-semibold opacity-80'>
                      {props.probeName}
                    </div>
                  )}
                  <div>{slotDescription}</div>
                </div>
              </TooltipContent>
            </Tooltip>
          )
        })}
      </div>
    </TooltipProvider>
  )
}

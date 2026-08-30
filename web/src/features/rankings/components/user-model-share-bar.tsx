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
import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { formatShare } from '../lib/format'
import {
  placeShareLabels,
  type ShareLabelPlacement,
} from '../lib/user-label-layout'
import type { UserRankingModel } from '../types'

const MODEL_COLORS = [
  'var(--chart-1)',
  'var(--chart-2)',
  'var(--chart-3)',
  'var(--chart-4)',
  'var(--chart-5)',
] as const
const OTHER_COLOR = 'var(--muted-foreground)'
const LABEL_LANE_HEIGHT = 24

type UserModelShareBarProps = {
  models: UserRankingModel[]
}

function modelShareColor(modelName: string): string {
  let hash = 0
  for (const character of modelName) {
    hash = (hash * 31 + (character.codePointAt(0) ?? 0)) >>> 0
  }
  return MODEL_COLORS[hash % MODEL_COLORS.length]
}

export function UserModelShareBar(props: UserModelShareBarProps) {
  const { t } = useTranslation()
  const labelsContainerRef = useRef<HTMLDivElement>(null)
  const labelRefs = useRef<Array<HTMLDivElement | null>>([])
  const [placements, setPlacements] = useState<ShareLabelPlacement[]>([])

  const items = useMemo(() => {
    let cumulativeShare = 0

    return props.models
      .filter((model) => model.share > 0)
      .map((model) => {
        const share = Math.min(1, Math.max(0, model.share))
        const label = model.is_other ? t('Others') : model.model_name
        const item = {
          ...model,
          share,
          label,
          anchor: cumulativeShare + share / 2,
          color: model.is_other
            ? OTHER_COLOR
            : modelShareColor(model.model_name),
        }
        cumulativeShare += share
        return item
      })
  }, [props.models, t])

  useLayoutEffect(() => {
    const container = labelsContainerRef.current
    if (!container || items.length === 0) return

    let animationFrameId: number | undefined
    const measure = () => {
      const containerWidth = container.getBoundingClientRect().width
      const measurements = items.map((item, index) => ({
        anchor: item.anchor,
        width: labelRefs.current[index]?.getBoundingClientRect().width ?? 0,
      }))
      setPlacements(placeShareLabels(containerWidth, measurements))
    }

    measure()

    const observer = new ResizeObserver(() => {
      if (animationFrameId !== undefined) {
        window.cancelAnimationFrame(animationFrameId)
      }
      animationFrameId = window.requestAnimationFrame(measure)
    })
    observer.observe(container)

    return () => {
      observer.disconnect()
      if (animationFrameId !== undefined) {
        window.cancelAnimationFrame(animationFrameId)
      }
    }
  }, [items])

  const laneCount =
    placements.length === 0
      ? 1
      : Math.max(...placements.map((placement) => placement.lane)) + 1

  return (
    <div className='min-w-0'>
      <div
        role='img'
        aria-label={t('Model usage distribution')}
        className='bg-muted flex h-2 w-full overflow-hidden rounded-sm'
      >
        {items.map((item) => (
          <span
            key={`${item.is_other ? 'other' : 'model'}-${item.model_name}`}
            data-share-segment
            aria-hidden='true'
            className='h-full'
            style={{
              backgroundColor: item.color,
              width: `${item.share * 100}%`,
            }}
          />
        ))}
      </div>

      <div
        ref={labelsContainerRef}
        data-testid='share-labels'
        className='relative mt-2 w-full transition-[height] duration-150 motion-reduce:transition-none'
        style={{ height: `${laneCount * LABEL_LANE_HEIGHT}px` }}
      >
        {items.map((item, index) => {
          const placement = placements[index] ?? { left: 0, lane: 0 }
          const percentage = formatShare(item.share)
          const accessibleLabel = `${item.label} ${percentage}`

          return (
            <div
              key={`${item.is_other ? 'other' : 'model'}-${item.model_name}`}
              ref={(element) => {
                labelRefs.current[index] = element
              }}
              data-share-label
              data-lane={placement.lane}
              tabIndex={0}
              title={accessibleLabel}
              aria-label={accessibleLabel}
              className='text-muted-foreground focus-visible:ring-ring absolute inline-flex max-w-full items-center gap-1 whitespace-nowrap text-[11px] leading-4 focus-visible:rounded-sm focus-visible:ring-2 focus-visible:outline-none'
              style={{
                left: `${placement.left}px`,
                top: `${placement.lane * LABEL_LANE_HEIGHT}px`,
              }}
            >
              <span
                aria-hidden='true'
                className='size-2 shrink-0 rounded-full'
                style={{ backgroundColor: item.color }}
              />
              <span className='max-w-32 truncate sm:max-w-48'>
                {item.label}
              </span>
              <span className='text-foreground shrink-0 font-mono tabular-nums'>
                {percentage}
              </span>
            </div>
          )
        })}
      </div>
    </div>
  )
}

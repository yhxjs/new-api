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
  Brain,
  ChevronDown,
  ChevronUp,
  FileText,
  Play,
  Server,
  Zap,
} from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'

import { triggerDegradationTargetRun } from '../api'
import { DEGRADED_PASS_RATE, HEALTHY_PASS_RATE } from '../constants'
import type { DegradationModelItem } from '../types'
import { LogDetailsDialog } from './log-details-dialog'
import { TimelineStrip } from './timeline-strip'

export type ModelChannelInstance = {
  channelId: number
  channelName: string
  model: DegradationModelItem
}

type ChannelModelCardProps = {
  modelName: string
  instances: ModelChannelInstance[]
  isAdmin?: boolean
  canReadLogs?: boolean
  canOperate?: boolean
}

export function ChannelModelCard(props: ChannelModelCardProps) {
  const { t } = useTranslation()
  const canReadLogs = props.canReadLogs ?? props.isAdmin ?? false
  const canOperate = props.canOperate ?? props.isAdmin ?? false
  const [expanded, setExpanded] = useState(true)
  const detailsId = useId()
  const [logDialogOpen, setLogDialogOpen] = useState(false)
  const [activeChannelId, setActiveChannelId] = useState<number | undefined>(
    undefined
  )
  const [testingChannelId, setTestingChannelId] = useState<number | null>(null)

  // 综合通过率与延迟（有执行数时按执行数加权，避免低频节点稀释结果）
  const totalInstances = props.instances.length
  const totalExecutions = props.instances.reduce(
    (acc, inst) => acc + (inst.model.total_executions ?? 0),
    0
  )
  const weightedAvg = (pick: (m: DegradationModelItem) => number) =>
    totalExecutions > 0
      ? props.instances.reduce(
          (acc, inst) =>
            acc + pick(inst.model) * (inst.model.total_executions ?? 0),
          0
        ) / totalExecutions
      : 0
  const avgPassRate =
    totalExecutions > 0 ? weightedAvg((m) => m.pass_rate) : null
  const avgLatency =
    totalInstances > 0 ? Math.round(weightedAvg((m) => m.avg_latency_ms)) : 0

  const handleTestChannelModel = async (channelId: number) => {
    try {
      setTestingChannelId(channelId)
      const res = await triggerDegradationTargetRun(channelId, props.modelName)
      if (res.success) {
        toast.success(res.message || t('Probe executed successfully'))
      } else {
        toast.error(res.message || t('Failed to test probe'))
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err)
      toast.error(msg || t('Failed to test probe'))
    } finally {
      setTestingChannelId(null)
    }
  }

  const handleOpenLogs = (channelId: number) => {
    setActiveChannelId(channelId)
    setLogDialogOpen(true)
  }

  const getHealthBadge = (rate: number | null) => {
    if (rate === null) {
      return {
        label: t('No Data'),
        className: 'bg-muted text-muted-foreground',
      }
    }
    if (rate >= HEALTHY_PASS_RATE) {
      return {
        label: t('Healthy'),
        className: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
      }
    }
    if (rate >= DEGRADED_PASS_RATE) {
      return {
        label: t('Degradation Risk'),
        className: 'bg-amber-500/10 text-amber-600 dark:text-amber-400',
      }
    }
    return {
      label: t('Critical Issue'),
      className: 'bg-rose-500/10 text-rose-600 dark:text-rose-400',
    }
  }

  const getPassRateColor = (rate: number | null) => {
    if (rate === null) return 'text-muted-foreground'
    if (rate >= HEALTHY_PASS_RATE) return 'text-emerald-500'
    if (rate >= DEGRADED_PASS_RATE) return 'text-amber-500'
    return 'text-rose-500'
  }

  const healthBadge = getHealthBadge(avgPassRate)

  return (
    <div className='bg-card/60 hover:border-primary/20 rounded-2xl border shadow-xs backdrop-blur-xs transition'>
      {/* 头部摘要栏 */}
      <div className='flex flex-col justify-between gap-3 p-4 sm:flex-row sm:items-center'>
        <div className='flex items-center gap-3'>
          <div className='bg-primary/10 text-primary flex h-9 w-9 items-center justify-center rounded-xl font-mono text-xs font-bold'>
            AI
          </div>
          <div>
            <div className='flex items-center gap-2'>
              <span className='text-foreground font-mono text-sm font-bold'>
                {props.modelName}
              </span>
              <span
                className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${healthBadge.className}`}
              >
                {healthBadge.label}
              </span>
            </div>
            <div className='text-muted-foreground mt-0.5 flex items-center gap-3 text-xs'>
              <span className='flex items-center gap-1'>
                <Server className='h-3 w-3' />
                {t('{{count}} Nodes', { count: totalInstances })}
              </span>
              <span className='flex items-center gap-1 font-mono'>
                <Zap className='h-3 w-3 text-amber-500' />
                {avgLatency > 0 ? `${avgLatency}ms` : t('N/A')}
              </span>
            </div>
          </div>
        </div>

        <div className='flex items-center gap-3'>
          <div className='text-right'>
            <div className='text-muted-foreground text-xs'>
              {t('Pass Rate')}
            </div>
            <div
              className={`font-mono text-base font-bold ${getPassRateColor(
                avgPassRate
              )}`}
            >
              {avgPassRate === null
                ? t('No Data')
                : `${avgPassRate.toFixed(1)}%`}
            </div>
          </div>

          <Button
            size='icon-sm'
            variant='ghost'
            aria-label={`${expanded ? t('Collapse') : t('Expand')}: ${props.modelName}`}
            aria-expanded={expanded}
            aria-controls={expanded ? detailsId : undefined}
            onClick={() => setExpanded(!expanded)}
          >
            {expanded ? (
              <ChevronUp className='h-4 w-4' />
            ) : (
              <ChevronDown className='h-4 w-4' />
            )}
          </Button>
        </div>
      </div>

      {/* 展开的渠道节点列表 */}
      {expanded && (
        <div
          id={detailsId}
          className='divide-border/60 bg-muted/10 divide-y border-t'
        >
          {props.instances.map((instance) => {
            const isTesting = testingChannelId === instance.channelId
            // 每个探针各渲染一条时间线；无探针配置时回退为聚合条
            const probeStrips =
              instance.model.probes.length > 0
                ? instance.model.probes
                : [
                    {
                      probe_id: 'aggregated',
                      probe_name: t('Aggregated Probes'),
                      pass_rate: instance.model.pass_rate,
                      slots: Array(24).fill(0) as number[],
                    },
                  ]

            return (
              <div
                // 非管理员脱敏后 channelId 恒为 0，须靠渠道名区分同模型的多节点
                key={`${instance.channelId}-${instance.channelName}`}
                className='space-y-2.5 p-3.5 text-xs'
              >
                <div className='flex flex-col justify-between gap-2 sm:flex-row sm:items-center'>
                  <div className='flex items-center gap-2'>
                    <span className='text-foreground font-medium'>
                      {instance.channelName}
                    </span>
                    {instance.channelId > 0 && props.isAdmin && (
                      <span className='bg-muted text-muted-foreground rounded px-1.5 py-0.5 font-mono text-[10px]'>
                        #{instance.channelId}
                      </span>
                    )}
                    {/* 仅管理员可见思考等级 */}
                    {props.isAdmin &&
                      instance.model.reasoning_effort &&
                      instance.model.reasoning_effort !== 'off' && (
                        <span className='flex items-center gap-1 rounded-full bg-purple-500/10 px-2 py-0.5 font-mono text-[10px] text-purple-600 dark:text-purple-400'>
                          <Brain className='h-3 w-3' />
                          {instance.model.reasoning_effort}
                        </span>
                      )}
                  </div>

                  <div className='flex items-center gap-2'>
                    <span className='text-muted-foreground mr-2 font-mono text-[11px]'>
                      {instance.model.avg_latency_ms > 0
                        ? `${instance.model.avg_latency_ms}ms`
                        : ''}
                    </span>

                    {(canReadLogs || canOperate) && (
                      <>
                        {canReadLogs && (
                          <Button
                            size='sm'
                            variant='ghost'
                            className='text-muted-foreground hover:text-foreground h-7 px-2 text-xs'
                            onClick={() => handleOpenLogs(instance.channelId)}
                          >
                            <FileText className='mr-1 h-3.5 w-3.5' />
                            {t('Logs')}
                          </Button>
                        )}

                        {canOperate && (
                          <Button
                            size='sm'
                            variant='ghost'
                            className='text-muted-foreground hover:text-foreground h-7 px-2 text-xs'
                            disabled={isTesting || instance.channelId <= 0}
                            onClick={() =>
                              handleTestChannelModel(instance.channelId)
                            }
                          >
                            <Play className='mr-1 h-3.5 w-3.5' />
                            {isTesting ? t('Testing...') : t('Probe')}
                          </Button>
                        )}
                      </>
                    )}
                  </div>
                </div>

                {/* 24 小时状态时间槽（每个探针一条） */}
                <div className='space-y-2'>
                  {probeStrips.map((probe) => (
                    <div key={probe.probe_id} className='space-y-1'>
                      <div className='text-muted-foreground flex items-center justify-between text-[10px]'>
                        <span>{probe.probe_name}</span>
                        <span className='font-mono'>
                          {(instance.model.total_executions ?? 0) > 0 &&
                          (instance.model.probes.length === 0 ||
                            probe.slots.some((slot) => slot > 0))
                            ? `${probe.pass_rate.toFixed(1)}% ${t('Pass')}`
                            : t('No Data')}
                        </span>
                      </div>
                      <TimelineStrip
                        slots={probe.slots}
                        probeName={probe.probe_name}
                      />
                    </div>
                  ))}
                </div>
              </div>
            )
          })}
        </div>
      )}

      {/* 日志抽屉/弹窗 */}
      <LogDetailsDialog
        open={logDialogOpen}
        onOpenChange={setLogDialogOpen}
        channelId={activeChannelId}
        modelName={props.modelName}
        isAdmin={canReadLogs}
      />
    </div>
  )
}

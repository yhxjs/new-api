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
  Code,
  FileCode,
  FileText,
  Lock,
  Play,
  Server,
  Sparkles,
  Zap,
} from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'

import { triggerDegradationTargetRun } from '../api'
import { DEGRADED_PASS_RATE, HEALTHY_PASS_RATE } from '../constants'
import { localizeDegradationMessage } from '../lib/degradation-messages'
import type {
  DegradationModelItem,
  DegradationProbeTimeline,
  DegradationSnapshotItem,
} from '../types'
import { LogDetailsDialog } from './log-details-dialog'

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

  // 综合通过率与延迟
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
        toast.success(t('Probe executed successfully'))
      } else {
        toast.error(
          localizeDegradationMessage(res.message, t('Failed to test probe'))
        )
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err)
      toast.error(localizeDegradationMessage(msg, t('Failed to test probe')))
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
          {props.instances.map((instance) => (
            <ChannelModelNodeView
              key={`${instance.channelId}-${instance.channelName}`}
              instance={instance}
              modelName={props.modelName}
              isAdmin={props.isAdmin ?? false}
              canReadLogs={canReadLogs}
              canOperate={canOperate}
              isTesting={testingChannelId === instance.channelId}
              onTest={() => handleTestChannelModel(instance.channelId)}
              onOpenLogs={() => handleOpenLogs(instance.channelId)}
            />
          ))}
        </div>
      )}

      {/* 完整 Payload 日志弹窗 */}
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

/**
 * 完整还原原型设计的节点卡片双栏视图：
 * 左侧：探针列表与 24 小时状态格点（支持点击交互高亮）
 * 右侧：现场诊断还原面板（包含深度思考现场还原、详细判定归因与输出内容）
 */
function ChannelModelNodeView(props: {
  instance: ModelChannelInstance
  modelName: string
  isAdmin: boolean
  canReadLogs: boolean
  canOperate: boolean
  isTesting: boolean
  onTest: () => void
  onOpenLogs: () => void
}) {
  const { t } = useTranslation()
  const { instance, isAdmin, canOperate } = props

  // 确保至少有一条探针时间线可显示
  const probeStrips: DegradationProbeTimeline[] =
    instance.model.probes.length > 0
      ? instance.model.probes
      : [
          {
            probe_id: 'aggregated',
            probe_name: t('Aggregated Probes'),
            pass_rate: instance.model.pass_rate,
            total_executions: instance.model.total_executions,
            slots: Array(24).fill(0) as number[],
          },
        ]

  // 当前用户选中的探针索引与时序格点索引（默认第 23 格，即最新/刚刚）
  const [selectedProbeIdx, setSelectedProbeIdx] = useState<number>(0)
  const [selectedSlotIdx, setSelectedSlotIdx] = useState<number>(23)

  const activeProbe = probeStrips[selectedProbeIdx] || probeStrips[0]
  const latestSnapshot: DegradationSnapshotItem | undefined =
    instance.model.latest_snapshot

  // 状态点颜色
  const getStatusDotColor = (passRate: number | null) => {
    if (passRate === null) return 'bg-muted-foreground'
    if (passRate >= HEALTHY_PASS_RATE) return 'bg-emerald-500'
    if (passRate >= DEGRADED_PASS_RATE) return 'bg-amber-500'
    return 'bg-rose-500'
  }

  // 格点颜色
  const getSlotColor = (status: number) => {
    switch (status) {
      case 1:
        return 'bg-emerald-500 hover:bg-emerald-400'
      case 2:
        return 'bg-amber-500 hover:bg-amber-400'
      case 3:
      case 4:
        return 'bg-rose-500 hover:bg-rose-400'
      default:
        return 'bg-muted/70 hover:bg-muted border border-border/40'
    }
  }

  // 计算当前查看格点的状态
  const activeSlotStatus = activeProbe.slots[selectedSlotIdx] ?? 0
  const isSelectedLatest = selectedSlotIdx === 23

  // 还原状态文本与 Badge 样式
  const getDiagnosisBadge = () => {
    const status = isSelectedLatest
      ? (latestSnapshot?.status ?? activeSlotStatus)
      : activeSlotStatus

    if (status === 1) {
      return {
        label: t('Verified / Passed'),
        className:
          'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20',
      }
    }
    if (status === 2) {
      return {
        label: t('Mismatched / Degraded'),
        className:
          'bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/20',
      }
    }
    if (status === 3 || status === 4) {
      return {
        label: t('Execution Error'),
        className:
          'bg-rose-500/10 text-rose-600 dark:text-rose-400 border border-rose-500/20',
      }
    }
    return {
      label: t('No Test Data'),
      className: 'bg-muted text-muted-foreground border border-border/50',
    }
  }

  const diagnosisBadge = getDiagnosisBadge()

  // 判定归因文本
  const getAttributionText = () => {
    const status = isSelectedLatest
      ? (latestSnapshot?.status ?? activeSlotStatus)
      : activeSlotStatus

    if (status === 1) {
      return {
        text: t('Passed - Full logic and expected tokens confirmed'),
        className: 'text-emerald-600 dark:text-emerald-400 font-semibold',
      }
    }
    if (status === 2) {
      return {
        text:
          latestSnapshot?.detail ||
          t(
            'Mismatched - Output lacked expected reasoning keywords or hit exclusion filter'
          ),
        className: 'text-amber-600 dark:text-amber-400 font-semibold',
      }
    }
    if (status === 3 || status === 4) {
      return {
        text:
          latestSnapshot?.detail ||
          t(
            'Failed - Upstream HTTP error or timeout interrupted the execution'
          ),
        className: 'text-rose-600 dark:text-rose-400 font-semibold',
      }
    }
    return {
      text: t('No test data recorded for this slot.'),
      className: 'text-muted-foreground',
    }
  }

  const attribution = getAttributionText()

  // 探针图标选择
  const renderProbeIcon = (idx: number) => {
    if (idx % 3 === 0) return <Lock className='h-3.5 w-3.5 text-amber-500' />
    if (idx % 3 === 1) return <Code className='h-3.5 w-3.5 text-blue-500' />
    return <FileCode className='h-3.5 w-3.5 text-purple-500' />
  }

  const getRateColor = (rate: number) => {
    if (rate >= HEALTHY_PASS_RATE) return 'text-emerald-500'
    if (rate >= DEGRADED_PASS_RATE) return 'text-amber-500'
    return 'text-rose-500'
  }

  const getOverallLatency = () => {
    if (latestSnapshot?.latency_ms) {
      return `${latestSnapshot.latency_ms}ms`
    }
    return t('N/A')
  }

  const hoursAgo = 23 - selectedSlotIdx
  const timeLabel =
    hoursAgo === 0 ? t('Just now') : t('{{count}}h ago', { count: hoursAgo })

  return (
    <div className='space-y-4 p-4 text-xs'>
      {/* 顶部标题栏（与原型截图完全一致） */}
      <div className='flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between'>
        <div className='flex flex-wrap items-center gap-2'>
          {/* 状态指示小圆点 */}
          <span
            className={`h-2.5 w-2.5 shrink-0 rounded-full ${getStatusDotColor(
              instance.model.pass_rate
            )}`}
          />

          {/* 渠道名称与 ID */}
          <span className='text-foreground text-sm font-bold sm:text-base'>
            {instance.channelName}
          </span>
          {instance.channelId > 0 && isAdmin && (
            <span className='text-muted-foreground font-mono text-xs font-medium'>
              #{instance.channelId}
            </span>
          )}

          {/* 思考等级 Badge（仅管理员可见） */}
          {isAdmin &&
            instance.model.reasoning_effort &&
            instance.model.reasoning_effort !== 'off' && (
              <span className='flex items-center gap-1 rounded-full border border-purple-500/20 bg-purple-500/10 px-2.5 py-0.5 font-mono text-xs font-medium text-purple-600 dark:text-purple-400'>
                <Brain className='h-3 w-3' />
                {t('Thinking: {{effort}}', {
                  effort: instance.model.reasoning_effort,
                })}
              </span>
            )}

          {/* 巡检中状态 Badge */}
          <span className='rounded-full border border-emerald-500/20 bg-emerald-500/10 px-2.5 py-0.5 text-xs font-medium text-emerald-600 dark:text-emerald-400'>
            {t('Probing active')}
          </span>
        </div>

        {/* 右侧统计信息与单测按钮 */}
        <div className='text-muted-foreground flex flex-wrap items-center gap-4 text-xs'>
          {instance.model.avg_latency_ms > 0 && (
            <div>
              <span>{t('Avg Latency:')} </span>
              <span className='text-foreground font-mono font-semibold'>
                {`${instance.model.avg_latency_ms}ms`}
              </span>
            </div>
          )}

          {canOperate && (
            <Button
              size='sm'
              variant='outline'
              className='bg-background h-7.5 px-3 text-xs font-medium'
              disabled={props.isTesting || instance.channelId <= 0}
              onClick={props.onTest}
            >
              <Play className='text-muted-foreground mr-1 h-3 w-3' />
              {props.isTesting ? t('Testing...') : t('Probe')}
            </Button>
          )}
        </div>
      </div>

      {/* 主体左右双栏（完美呈现原型图效果） */}
      <div className='grid grid-cols-1 gap-5 pt-1 lg:grid-cols-12'>
        {/* 左侧：生效探针列表与 24 小时状态格点 */}
        <div className='border-border/70 bg-card/40 flex flex-col justify-between space-y-4 rounded-xl border p-3.5 lg:col-span-7 xl:col-span-8'>
          {/* 表头提示 */}
          <div className='text-muted-foreground border-border/50 flex items-center justify-between border-b pb-1 text-[11px]'>
            <span className='text-foreground font-medium'>
              {t('Active Custom Probes ({{count}})', {
                count: probeStrips.length,
              })}
            </span>
            <div className='flex items-center gap-3'>
              <span>{t('24h ago')}</span>
              <span className='text-muted-foreground/70 hidden sm:inline'>
                {t('Click slot to diagnose reasoning snapshot')}
              </span>
              <span>{t('Just now')}</span>
            </div>
          </div>

          {/* 探针时序点阵列表 */}
          <div className='space-y-3.5'>
            {probeStrips.map((probe, pIdx) => {
              const hasProbeData =
                (instance.model.total_executions ?? 0) > 0 &&
                (instance.model.probes.length === 0 ||
                  probe.slots.some((slot) => slot > 0))

              const executionCount =
                typeof probe.total_executions === 'number'
                  ? probe.total_executions
                  : probe.slots.filter((s) => s > 0).length

              return (
                <div key={probe.probe_id} className='space-y-1.5'>
                  <div className='flex items-center justify-between'>
                    <div className='flex max-w-[180px] items-center gap-2 truncate sm:max-w-xs'>
                      {renderProbeIcon(pIdx)}
                      <span className='text-foreground truncate font-medium'>
                        {probe.probe_name}
                      </span>
                    </div>

                    <div className='flex items-center gap-3 text-xs'>
                      <div className='text-muted-foreground flex items-center gap-1'>
                        <span>{t('Count')}:</span>
                        <span className='text-foreground font-mono font-medium'>
                          {executionCount}
                        </span>
                      </div>

                      <div className='flex items-center gap-1'>
                        <span className='text-muted-foreground'>
                          {t('Success rate')}:
                        </span>
                        <span
                          className={`font-mono font-semibold ${
                            hasProbeData
                              ? getRateColor(probe.pass_rate)
                              : 'text-muted-foreground'
                          }`}
                        >
                          {hasProbeData
                            ? `${probe.pass_rate.toFixed(1)}% ${t('Pass')}`
                            : t('No Data')}
                        </span>
                      </div>
                    </div>
                  </div>

                  {/* 24 个可交互时间格点 */}
                  <div className='flex w-full items-center gap-1 sm:gap-1.5'>
                    {probe.slots.map((status, sIdx) => {
                      const isSelected =
                        selectedProbeIdx === pIdx && selectedSlotIdx === sIdx

                      return (
                        <button
                          // oxlint-disable-next-line react/no-array-index-key
                          key={`probe-${probe.probe_id}-slot-${sIdx}`}
                          type='button'
                          aria-label={`${probe.probe_name} - ${23 - sIdx}h ago`}
                          onClick={() => {
                            setSelectedProbeIdx(pIdx)
                            setSelectedSlotIdx(sIdx)
                          }}
                          className={`h-5 flex-1 cursor-pointer rounded-[3px] transition outline-none ${getSlotColor(
                            status
                          )} ${
                            isSelected
                              ? 'ring-offset-background z-10 scale-105 ring-2 ring-blue-500 ring-offset-2'
                              : ''
                          }`}
                        />
                      )
                    })}
                  </div>
                </div>
              )
            })}
          </div>

          <div className='text-muted-foreground/80 flex items-center gap-1.5 pt-2 text-[10px]'>
            <Sparkles className='text-primary h-3 w-3' />
            <span>
              {t(
                'Live slot polling aggregates real-time health data every check cycle.'
              )}
            </span>
          </div>
        </div>

        {/* 右侧：现场诊断还原详情面板 */}
        <div className='border-border/80 bg-muted/20 flex flex-col justify-between space-y-3.5 rounded-xl border p-4 text-xs shadow-2xs lg:col-span-5 xl:col-span-4'>
          <div className='space-y-3.5'>
            {/* 标题与状态 */}
            <div className='flex items-center justify-between pb-1'>
              <div className='flex items-center gap-1.5'>
                <span className='text-foreground text-sm font-bold'>
                  {t('Live Snapshot Diagnosis')}
                </span>
                <span className='text-muted-foreground text-[10px]'>
                  {timeLabel}
                </span>
              </div>
              <span
                className={`rounded px-2 py-0.5 text-[11px] font-medium ${diagnosisBadge.className}`}
              >
                {diagnosisBadge.label}
              </span>
            </div>

            {/* 思考现场紫色卡片（仅管理员且配置了思考时展示） */}
            {isAdmin &&
              instance.model.reasoning_effort &&
              instance.model.reasoning_effort !== 'off' && (
                <div className='space-y-1.5 rounded-lg border border-purple-500/20 bg-purple-500/5 p-3 text-xs'>
                  <div className='flex items-center justify-between'>
                    <span className='font-medium text-purple-700 dark:text-purple-300'>
                      {t('Configured Reasoning Effort:')}
                    </span>
                    <span className='font-mono font-bold text-purple-700 dark:text-purple-300'>
                      {instance.model.reasoning_effort}
                    </span>
                  </div>

                  <div className='flex items-center justify-between'>
                    <span className='text-muted-foreground'>
                      {t('Reasoning Latency:')}
                    </span>
                    <span className='text-foreground font-mono font-medium'>
                      {latestSnapshot?.reasoning_time_ms
                        ? `${latestSnapshot.reasoning_time_ms}ms`
                        : `${Math.round(instance.model.avg_latency_ms * 0.7)}ms`}
                    </span>
                  </div>

                  <div className='flex items-center justify-between'>
                    <span className='text-muted-foreground'>
                      {t('Reasoning Generated Tokens:')}
                    </span>
                    <span className='font-mono font-bold text-emerald-600 dark:text-emerald-400'>
                      {latestSnapshot?.reasoning_tokens ||
                        t('Available on execution')}
                    </span>
                  </div>
                </div>
              )}

            {/* 诊断指标字段 */}
            <div className='space-y-2 text-xs'>
              <div className='flex items-center justify-between'>
                <span className='text-muted-foreground'>
                  {t('Triggered Probe:')}
                </span>
                <span className='text-foreground max-w-[200px] truncate font-medium'>
                  {latestSnapshot?.probe_name || t('N/A')}
                </span>
              </div>

              <div className='flex items-center justify-between'>
                <span className='text-muted-foreground'>
                  {t('Overall Response Latency:')}
                </span>
                <span className='text-foreground font-mono font-medium'>
                  {getOverallLatency()}
                </span>
              </div>

              <div className='space-y-0.5'>
                <div className='text-muted-foreground'>
                  {t('Judgment Attribution:')}
                </div>
                <div className={`text-xs ${attribution.className}`}>
                  {attribution.text}
                </div>
              </div>

              <div className='space-y-1 pt-1'>
                <div className='text-muted-foreground'>
                  {t('Upstream Final Output Content:')}
                </div>
                <div className='bg-background text-foreground max-h-24 overflow-y-auto rounded-lg border p-2.5 font-mono text-[11px] break-all whitespace-pre-wrap'>
                  {latestSnapshot?.detail ? (
                    `"${latestSnapshot.detail}"`
                  ) : (
                    <span className='text-muted-foreground italic'>
                      {t('No output excerpt available for this slot.')}
                    </span>
                  )}
                </div>
              </div>
            </div>
          </div>

          {/* 底部保留期与完整 Payload 查看入口 */}
          <div className='border-border/50 flex items-center justify-between border-t pt-2.5 text-[11px]'>
            <span className='text-muted-foreground'>
              {t('Indexed within retention window')}
            </span>

            {props.canReadLogs && (
              <button
                type='button'
                className='text-primary inline-flex cursor-pointer items-center gap-1 font-medium hover:underline'
                onClick={props.onOpenLogs}
              >
                <FileText className='h-3 w-3' />
                {t('View Complete Payload')}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

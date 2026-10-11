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
  Activity,
  AlertTriangle,
  CheckCircle2,
  Clock,
  Layers,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { DEGRADED_PASS_RATE, HEALTHY_PASS_RATE } from '../constants'
import type { DegradationOverviewStats } from '../types'

type StatusSummaryProps = {
  stats?: DegradationOverviewStats
}

export function StatusSummary(props: StatusSummaryProps) {
  const { t } = useTranslation()
  const stats = props.stats || {
    pass_rate: 100,
    total_executions: 0,
    mismatch_count: 0,
    error_count: 0,
    active_targets: 0,
    next_run_seconds: 0,
    enabled: false,
    aggregation_error: false,
  }

  const formatCountdown = (seconds: number) => {
    if (seconds <= 0) return t('Executing...')
    const mins = Math.floor(seconds / 60)
    const secs = seconds % 60
    return `${mins}m ${secs}s`
  }

  const getPassRateTextColor = (rate: number) => {
    if (rate >= HEALTHY_PASS_RATE) return 'text-emerald-500'
    if (rate >= DEGRADED_PASS_RATE) return 'text-amber-500'
    return 'text-rose-500'
  }

  return (
    <div className='space-y-4'>
      {/* 聚合数据不完整警告 */}
      {stats.aggregation_error && (
        <div className='flex items-center gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3.5 py-2 text-xs text-amber-600 dark:text-amber-400'>
          <AlertTriangle className='h-3.5 w-3.5 shrink-0' />
          <span>
            {t(
              'Probe data failed to load completely; the figures below may be inaccurate.'
            )}
          </span>
        </div>
      )}

      {/* 顶部指标卡片网格 */}
      <div className='grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6'>
        {/* 通过率 */}
        <div className='bg-card/60 flex flex-col justify-between rounded-xl border p-3.5 shadow-xs backdrop-blur-sm transition hover:border-emerald-500/30'>
          <div className='text-muted-foreground flex items-center justify-between'>
            <span className='text-xs font-medium'>
              {t('Average Pass Rate')}
            </span>
            <CheckCircle2 className='h-4 w-4 text-emerald-500' />
          </div>
          <div className='mt-2 flex items-baseline gap-1'>
            {stats.total_executions > 0 ? (
              <span
                className={`font-mono text-2xl font-bold tracking-tight ${getPassRateTextColor(
                  stats.pass_rate
                )}`}
              >
                {stats.pass_rate.toFixed(1)}%
              </span>
            ) : (
              // 后端在零执行时默认返回 100%，直接展示会被误读为“全部健康”
              <span className='text-muted-foreground font-mono text-2xl font-bold tracking-tight'>
                {t('No Data')}
              </span>
            )}
          </div>
          <span className='text-muted-foreground mt-1 text-[10px]'>
            {t('24-Hour Window')}
          </span>
        </div>

        {/* 总执行次数 */}
        <div className='bg-card/60 hover:border-border flex flex-col justify-between rounded-xl border p-3.5 shadow-xs backdrop-blur-sm transition'>
          <div className='text-muted-foreground flex items-center justify-between'>
            <span className='text-xs font-medium'>{t('Total Executions')}</span>
            <Activity className='h-4 w-4 text-blue-500' />
          </div>
          <div className='mt-2 flex items-baseline gap-1'>
            <span className='text-foreground font-mono text-2xl font-bold tracking-tight'>
              {stats.total_executions}
            </span>
          </div>
          <span className='text-muted-foreground mt-1 text-[10px]'>
            {t('Probe Runs')}
          </span>
        </div>

        {/* 疑似降智 */}
        <div className='bg-card/60 flex flex-col justify-between rounded-xl border p-3.5 shadow-xs backdrop-blur-sm transition hover:border-amber-500/30'>
          <div className='text-muted-foreground flex items-center justify-between'>
            <span className='text-xs font-medium'>
              {t('Degraded / Mismatch')}
            </span>
            <AlertTriangle className='h-4 w-4 text-amber-500' />
          </div>
          <div className='mt-2 flex items-baseline gap-1'>
            <span
              className={`font-mono text-2xl font-bold tracking-tight ${
                stats.mismatch_count > 0 ? 'text-amber-500' : 'text-foreground'
              }`}
            >
              {stats.mismatch_count}
            </span>
          </div>
          <span className='text-muted-foreground mt-1 text-[10px]'>
            {t('Answer Mismatch')}
          </span>
        </div>

        {/* 失败/异常 */}
        <div className='bg-card/60 flex flex-col justify-between rounded-xl border p-3.5 shadow-xs backdrop-blur-sm transition hover:border-rose-500/30'>
          <div className='text-muted-foreground flex items-center justify-between'>
            <span className='text-xs font-medium'>
              {t('Errors & Timeouts')}
            </span>
            <XCircle className='h-4 w-4 text-rose-500' />
          </div>
          <div className='mt-2 flex items-baseline gap-1'>
            <span
              className={`font-mono text-2xl font-bold tracking-tight ${
                stats.error_count > 0 ? 'text-rose-500' : 'text-foreground'
              }`}
            >
              {stats.error_count}
            </span>
          </div>
          <span className='text-muted-foreground mt-1 text-[10px]'>
            {t('Connection Failures')}
          </span>
        </div>

        {/* 活跃检测目标 */}
        <div className='bg-card/60 hover:border-border flex flex-col justify-between rounded-xl border p-3.5 shadow-xs backdrop-blur-sm transition'>
          <div className='text-muted-foreground flex items-center justify-between'>
            <span className='text-xs font-medium'>{t('Active Targets')}</span>
            <Layers className='h-4 w-4 text-purple-500' />
          </div>
          <div className='mt-2 flex items-baseline gap-1'>
            <span className='text-foreground font-mono text-2xl font-bold tracking-tight'>
              {stats.active_targets}
            </span>
          </div>
          <span className='text-muted-foreground mt-1 text-[10px]'>
            {t('Enabled Channels & Models')}
          </span>
        </div>

        {/* 下次执行倒计时 */}
        <div className='bg-card/60 hover:border-border flex flex-col justify-between rounded-xl border p-3.5 shadow-xs backdrop-blur-sm transition'>
          <div className='text-muted-foreground flex items-center justify-between'>
            <span className='text-xs font-medium'>{t('Next Scheduled')}</span>
            <Clock className='h-4 w-4 text-emerald-500' />
          </div>
          <div className='mt-2 flex items-baseline gap-1'>
            <span className='text-foreground font-mono text-lg font-bold tracking-tight'>
              {!stats.enabled
                ? t('Not Enabled')
                : formatCountdown(stats.next_run_seconds)}
            </span>
          </div>
          <span className='text-muted-foreground mt-1 text-[10px]'>
            {t('Cron Auto-Polling')}
          </span>
        </div>
      </div>

      {/* 图例状态说明条 */}
      <div className='bg-muted/30 flex flex-wrap items-center justify-between gap-3 rounded-lg border px-3.5 py-2 text-xs'>
        <div className='text-muted-foreground flex items-center gap-1.5 font-medium'>
          <span>{t('Timeline Status Guide:')}</span>
        </div>
        <div className='text-muted-foreground flex flex-wrap items-center gap-4 text-[11px]'>
          <div className='flex items-center gap-1.5'>
            <span className='h-2.5 w-2.5 rounded-sm bg-emerald-500' />
            <span>{t('Normal / Verified (100%)')}</span>
          </div>
          <div className='flex items-center gap-1.5'>
            <span className='h-2.5 w-2.5 rounded-sm bg-amber-500' />
            <span>{t('Suspected Degradation / Mismatch')}</span>
          </div>
          <div className='flex items-center gap-1.5'>
            <span className='h-2.5 w-2.5 rounded-sm bg-rose-500' />
            <span>{t('Error / Timeout / Failed')}</span>
          </div>
          <div className='flex items-center gap-1.5'>
            <span className='bg-muted border-border/60 h-2.5 w-2.5 rounded-sm border' />
            <span>{t('No Test Data')}</span>
          </div>
        </div>
      </div>
    </div>
  )
}

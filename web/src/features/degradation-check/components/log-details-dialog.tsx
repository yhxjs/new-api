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
import { CheckCircle2, AlertTriangle, XCircle, RefreshCw } from 'lucide-react'
import { useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertAction, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'

import { getDegradationLogs } from '../api'
import type { DegradationCheckLog } from '../types'

type LogDetailsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channelId?: number
  modelName?: string
  isAdmin?: boolean
}

const PAGE_SIZE = 20

export function LogDetailsDialog(props: LogDetailsDialogProps) {
  const { t } = useTranslation()
  const detailsIdPrefix = useId()
  const [selectedLog, setSelectedLog] = useState<DegradationCheckLog | null>(
    null
  )
  const [page, setPage] = useState(1)

  // Switching target or reopening the dialog restarts at the first page.
  useEffect(() => {
    setPage(1)
    setSelectedLog(null)
  }, [props.open, props.channelId, props.modelName])

  const logsQuery = useQuery({
    queryKey: ['degradation-logs', props.channelId, props.modelName, page],
    queryFn: () =>
      getDegradationLogs({
        channel_id: props.channelId,
        model_name: props.modelName,
        page,
        page_size: PAGE_SIZE,
      }),
    enabled: props.open && (props.isAdmin ?? false),
  })

  const logs = logsQuery.data?.data?.items || []
  const total = logsQuery.data?.data?.total || 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const renderStatus = (status: number) => {
    switch (status) {
      case 1:
        return (
          <span className='inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 text-[11px] font-medium text-emerald-600 dark:text-emerald-400'>
            <CheckCircle2 className='h-3 w-3' />
            {t('Passed')}
          </span>
        )
      case 2:
        return (
          <span className='inline-flex items-center gap-1 rounded-full bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-600 dark:text-amber-400'>
            <AlertTriangle className='h-3 w-3' />
            {t('Mismatch')}
          </span>
        )
      default:
        return (
          <span className='inline-flex items-center gap-1 rounded-full bg-rose-500/10 px-2 py-0.5 text-[11px] font-medium text-rose-600 dark:text-rose-400'>
            <XCircle className='h-3 w-3' />
            {t('Failed')}
          </span>
        )
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='flex max-h-[85vh] max-w-3xl flex-col'>
        <DialogHeader>
          <div className='flex items-center justify-between pr-6'>
            <DialogTitle>
              {t('Degradation Probe Logs: {{model}}', {
                model: props.modelName || t('All'),
              })}
            </DialogTitle>
            <Button
              size='icon-sm'
              variant='ghost'
              aria-label={t('Refresh')}
              onClick={() => logsQuery.refetch()}
              disabled={logsQuery.isFetching}
            >
              <RefreshCw
                className={`h-3.5 w-3.5 ${logsQuery.isFetching ? 'animate-spin' : ''}`}
              />
            </Button>
          </div>
        </DialogHeader>

        <div className='flex-1 space-y-3 overflow-y-auto py-2 pr-1 text-xs'>
          {logsQuery.isError && (
            <Alert variant='destructive'>
              <AlertTitle>
                {logsQuery.data
                  ? t(
                      'Failed to refresh logs. Showing the last successful results.'
                    )
                  : t('Failed to load logs')}
              </AlertTitle>
              <AlertAction>
                <Button
                  size='sm'
                  variant='outline'
                  onClick={() => logsQuery.refetch()}
                  disabled={logsQuery.isFetching}
                >
                  {t('Retry')}
                </Button>
              </AlertAction>
            </Alert>
          )}

          {logsQuery.isLoading && (
            <div className='space-y-2'>
              <Skeleton className='h-12 w-full' />
              <Skeleton className='h-12 w-full' />
              <Skeleton className='h-12 w-full' />
            </div>
          )}

          {logsQuery.isSuccess && logs.length === 0 && (
            <div className='text-muted-foreground rounded-xl border border-dashed p-8 text-center'>
              {t('No probe logs recorded yet for this target.')}
            </div>
          )}

          {!logsQuery.isLoading &&
            logs.length > 0 &&
            logs.map((log) => {
              const isSelected = selectedLog?.id === log.id
              const detailsId = `${detailsIdPrefix}-${log.id}`
              const created = new Date(log.created_at)
              const createdDate = Number.isNaN(created.getTime())
                ? log.created_at
                : new Intl.DateTimeFormat(undefined, {
                    month: 'short',
                    day: 'numeric',
                    hour: '2-digit',
                    minute: '2-digit',
                    second: '2-digit',
                  }).format(created)

              const cardClassName = isSelected
                ? 'border-primary bg-primary/5'
                : 'bg-card/60 hover:bg-card'

              return (
                <div
                  key={log.id}
                  className={`space-y-2 rounded-xl border p-3 transition ${cardClassName}`}
                >
                  <Button
                    type='button'
                    variant='ghost'
                    className='h-auto w-full justify-between p-0 text-left font-normal whitespace-normal hover:bg-transparent aria-expanded:bg-transparent'
                    aria-expanded={isSelected}
                    aria-controls={isSelected ? detailsId : undefined}
                    onClick={() => setSelectedLog(isSelected ? null : log)}
                  >
                    <span className='flex items-center gap-2'>
                      {renderStatus(log.status)}
                      <span className='text-foreground font-semibold'>
                        {log.probe_name}
                      </span>
                      <span className='text-muted-foreground font-mono text-[10px]'>
                        {log.channel_name} (ID: {log.channel_id})
                      </span>
                    </span>

                    <span className='text-muted-foreground flex items-center gap-3 font-mono text-[11px]'>
                      <span>{log.latency_ms}ms</span>
                      <span>{createdDate}</span>
                    </span>
                  </Button>

                  {isSelected && (
                    <div
                      id={detailsId}
                      className='bg-muted/20 space-y-2.5 rounded-lg border-t p-2.5 pt-2 font-mono text-[11px]'
                    >
                      {log.detail && (
                        <div>
                          <span className='text-muted-foreground font-semibold'>
                            {t('Detail')}:{' '}
                          </span>
                          <span className='text-foreground break-all whitespace-pre-wrap'>
                            {log.detail}
                          </span>
                        </div>
                      )}

                      {props.isAdmin && log.reasoning_tokens > 0 && (
                        <div className='text-muted-foreground pt-1 text-[10px]'>
                          {t('Reasoning Tokens: {{count}}', {
                            count: log.reasoning_tokens,
                          })}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              )
            })}
        </div>

        {totalPages > 1 && (
          <div className='flex items-center justify-between border-t pt-2 text-xs'>
            <Button
              size='sm'
              variant='outline'
              disabled={page <= 1 || logsQuery.isFetching}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
            >
              {t('Previous')}
            </Button>
            <span className='text-muted-foreground font-mono'>
              {t('Page {{page}} of {{totalPages}}', { page, totalPages })}
            </span>
            <Button
              size='sm'
              variant='outline'
              disabled={page >= totalPages || logsQuery.isFetching}
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
            >
              {t('Next')}
            </Button>
          </div>
        )}

        <DialogFooter>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import type { Table } from '@tanstack/react-table'
import {
  Download,
  FileJson,
  FileSpreadsheet,
  FileText,
  History,
  Loader2,
  RefreshCw,
  Trash2,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Progress } from '@/components/ui/progress'
import { ScrollArea } from '@/components/ui/scroll-area'
import { getCurrencyDisplay } from '@/lib/currency'
import { cn } from '@/lib/utils'

import {
  createLogExport,
  deleteLogExport,
  downloadLogExport,
  listLogExports,
} from '../api'
import { LOG_CATEGORY_LABELS } from '../constants'
import {
  buildExportColumns,
  buildExportFilters,
  buildPresentationLabels,
  collectAutoDownloadTransitions,
  hasActiveLogExports,
  logExportFormatOptions,
} from '../lib/export'
import type {
  CreateLogExportRequest,
  LogCategory,
  LogExportFormat,
  LogExportTask,
} from '../types'
import { useLogsViewScope, useUsageLogsContext } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

const exportFormatIcons = {
  csv: FileSpreadsheet,
  json: FileJson,
  md: FileText,
}

const statusBadgeVariants: Record<
  LogExportTask['status'],
  'secondary' | 'warning' | 'default' | 'destructive'
> = {
  pending: 'secondary',
  running: 'warning',
  succeeded: 'default',
  failed: 'destructive',
}

function getCurrencyPresentation() {
  const { config, meta } = getCurrencyDisplay()
  if (meta.kind === 'tokens') {
    return {
      quota_per_unit: config.quotaPerUnit,
      quota_display_mode: 'quota',
    }
  }
  return {
    quota_per_unit: config.quotaPerUnit,
    currency_symbol: meta.symbol,
    currency_rate: meta.exchangeRate,
    quota_display_mode: 'currency',
  }
}

function formatBytes(bytes: number): string {
  if (!bytes) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

function formatTaskTime(timestamp: number): string {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString()
}

interface LogExportControlProps<TData> {
  table: Table<TData>
  logCategory: LogCategory
}

export function LogExportControl<TData>({
  table,
  logCategory,
}: LogExportControlProps<TData>) {
  const { t, i18n } = useTranslation()
  const searchParams = route.useSearch() as Record<string, unknown>
  const queryClient = useQueryClient()
  const { isAdminView } = useLogsViewScope()
  const { sensitiveVisible } = useUsageLogsContext()
  const [historyOpen, setHistoryOpen] = useState(false)
  const [historyPage, setHistoryPage] = useState(1)
  const autoDownloadTaskIds = useRef(new Set<string>())
  const handledTaskIds = useRef(new Set<string>())

  const exportsQuery = useQuery({
    queryKey: ['log-exports', 'recent'],
    queryFn: () => listLogExports(1, 20),
    refetchInterval: (query) => {
      const tasks = query.state.data?.data?.items ?? []
      return hasActiveLogExports(tasks) ? 3000 : false
    },
  })

  const historyQuery = useQuery({
    queryKey: ['log-exports', 'history', historyPage],
    queryFn: () => listLogExports(historyPage, 20),
    enabled: historyOpen,
    placeholderData: (previousData) => previousData,
    refetchInterval: (query) => {
      const historyTasks = query.state.data?.data?.items ?? []
      return hasActiveLogExports(historyTasks) ? 3000 : false
    },
  })

  const tasks = useMemo(
    () => exportsQuery.data?.data?.items ?? [],
    [exportsQuery.data?.data?.items]
  )
  const historyTasks = historyQuery.data?.data?.items ?? []
  const historyTotal = historyQuery.data?.data?.total ?? 0
  const historyPageCount = Math.max(1, Math.ceil(historyTotal / 20))
  const activeCount = tasks.filter(
    (task) => task.status === 'pending' || task.status === 'running'
  ).length

  const createMutation = useMutation({
    mutationFn: async (format: LogExportFormat) => {
      const columns = buildExportColumns(
        logCategory,
        table.getVisibleLeafColumns().map((column) => column.id),
        t
      )
      if (columns.length === 0) {
        throw new Error(t('No exportable columns are visible'))
      }
      const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
      const request: CreateLogExportRequest = {
        category: logCategory,
        format,
        scope: isAdminView ? 'all' : 'self',
        filters: buildExportFilters(
          logCategory,
          isAdminView,
          searchParams,
          table.getState().columnFilters
        ),
        columns,
        sensitive_visible: sensitiveVisible,
        presentation: {
          locale: i18n.resolvedLanguage || i18n.language || 'en',
          time_zone: timeZone,
          labels: buildPresentationLabels(logCategory, t),
          ...getCurrencyPresentation(),
        },
      }
      const result = await createLogExport(request)
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to create export task'))
      }
      return result.data
    },
    onSuccess: (task) => {
      autoDownloadTaskIds.current.add(task.task_id)
      toast.success(t('Export task created'))
      void queryClient.invalidateQueries({ queryKey: ['log-exports'] })
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? t(error.message, {
              defaultValue: t('Failed to create export task'),
            })
          : t('Failed to create export task')
      )
    },
  })

  const deleteMutation = useMutation({
    mutationFn: deleteLogExport,
    onSuccess: (result) => {
      if (!result.success) {
        toast.error(
          result.message
            ? t(result.message, {
                defaultValue: t('Failed to delete export task'),
              })
            : t('Failed to delete export task')
        )
        return
      }
      toast.success(t('Export task deleted'))
      void queryClient.invalidateQueries({ queryKey: ['log-exports'] })
    },
    onError: () => toast.error(t('Failed to delete export task')),
  })

  useEffect(() => {
    const transitions = collectAutoDownloadTransitions(
      tasks,
      autoDownloadTaskIds.current,
      handledTaskIds.current
    )
    transitions.succeeded.forEach((task) => {
      void downloadLogExport(task)
        .then(() => toast.success(t('Export downloaded')))
        .catch(() => toast.error(t('Failed to download export file')))
    })
    transitions.failed.forEach((task) => {
      toast.error(
        task.error
          ? t(task.error, { defaultValue: t('Export task failed') })
          : t('Export task failed')
      )
    })
  }, [tasks, t])

  const handleDownload = async (task: LogExportTask) => {
    try {
      await downloadLogExport(task)
      toast.success(t('Export downloaded'))
    } catch {
      toast.error(t('Failed to download export file'))
    }
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={createMutation.isPending}
              className='gap-1.5'
            />
          }
        >
          {createMutation.isPending ? (
            <Loader2 className='animate-spin' />
          ) : (
            <Download />
          )}
          <span className='hidden sm:inline'>{t('Export')}</span>
          {activeCount > 0 && (
            <Badge variant='secondary' className='h-5 min-w-5 px-1 text-[10px]'>
              {activeCount}
            </Badge>
          )}
        </DropdownMenuTrigger>
        <DropdownMenuContent align='end' className='w-52'>
          {logExportFormatOptions.map(({ format, label }) => {
            const FormatIcon = exportFormatIcons[format]
            return (
              <DropdownMenuItem
                key={format}
                onClick={() => createMutation.mutate(format)}
              >
                <FormatIcon />
                {t(label)}
              </DropdownMenuItem>
            )
          })}
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => setHistoryOpen(true)}>
            <History />
            {t('Export History')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <Dialog
        open={historyOpen}
        onOpenChange={(open) => {
          setHistoryOpen(open)
          if (!open) setHistoryPage(1)
        }}
      >
        <DialogContent className='sm:max-w-2xl'>
          <DialogHeader>
            <div className='flex items-center justify-between gap-3 pr-8'>
              <DialogTitle>{t('Export History')}</DialogTitle>
              <Button
                type='button'
                size='icon-sm'
                variant='ghost'
                onClick={() => historyQuery.refetch()}
                disabled={historyQuery.isFetching}
                aria-label={t('Refresh')}
              >
                <RefreshCw
                  className={cn(historyQuery.isFetching && 'animate-spin')}
                />
              </Button>
            </div>
            <DialogDescription>
              {t('Export files are retained for 24 hours.')}
            </DialogDescription>
          </DialogHeader>
          <ScrollArea className='max-h-[60vh] pr-2'>
            <div className='space-y-2'>
              {historyQuery.isLoading && (
                <div className='flex justify-center py-10'>
                  <Loader2 className='text-muted-foreground animate-spin' />
                </div>
              )}
              {!historyQuery.isLoading && historyTasks.length === 0 && (
                <div className='text-muted-foreground py-10 text-center'>
                  {t('No export tasks yet')}
                </div>
              )}
              {!historyQuery.isLoading &&
                historyTasks.map((task) => (
                  <div
                    key={task.task_id}
                    className='bg-muted/20 space-y-2 rounded-lg border p-3'
                  >
                    <div className='flex flex-wrap items-start justify-between gap-2'>
                      <div className='min-w-0'>
                        <div className='flex flex-wrap items-center gap-2'>
                          <span className='font-medium uppercase'>
                            {task.format}
                          </span>
                          <span className='text-muted-foreground text-xs'>
                            {t(LOG_CATEGORY_LABELS[task.category])}
                          </span>
                          <span className='text-muted-foreground text-xs'>
                            {task.scope === 'all' ? t('All') : t('Only Mine')}
                          </span>
                          <Badge variant={statusBadgeVariants[task.status]}>
                            {t(task.status)}
                          </Badge>
                        </div>
                        <div className='text-muted-foreground mt-1 text-xs'>
                          {formatTaskTime(task.created_at)}
                          {' · '}
                          {task.processed_rows.toLocaleString()} {t('rows')}
                          {' · '}
                          {formatBytes(task.file_size)}
                        </div>
                      </div>
                      <div className='flex items-center gap-1'>
                        <Button
                          type='button'
                          size='icon-sm'
                          variant='ghost'
                          disabled={
                            task.status !== 'succeeded' ||
                            (task.expires_at > 0 &&
                              task.expires_at <= Math.floor(Date.now() / 1000))
                          }
                          onClick={() => handleDownload(task)}
                          aria-label={t('Download')}
                        >
                          <Download />
                        </Button>
                        <Button
                          type='button'
                          size='icon-sm'
                          variant='ghost'
                          disabled={
                            task.status === 'pending' ||
                            task.status === 'running' ||
                            deleteMutation.isPending
                          }
                          onClick={() => deleteMutation.mutate(task.task_id)}
                          aria-label={t('Delete')}
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    </div>
                    {(task.status === 'pending' ||
                      task.status === 'running') && (
                      <div className='flex items-center gap-2'>
                        <Progress value={task.progress} className='flex-1' />
                        <span className='text-muted-foreground w-9 text-right text-xs tabular-nums'>
                          {task.progress}%
                        </span>
                      </div>
                    )}
                    {task.error && (
                      <div className='text-destructive text-xs'>
                        {t(task.error, {
                          defaultValue: t('Export task failed'),
                        })}
                      </div>
                    )}
                    {(task.status === 'succeeded' ||
                      task.status === 'failed') &&
                      task.expires_at > 0 && (
                        <div className='text-muted-foreground text-xs'>
                          {t('Expires at')}: {formatTaskTime(task.expires_at)}
                        </div>
                      )}
                  </div>
                ))}
            </div>
            {historyTotal > 20 && (
              <div className='mt-3 flex items-center justify-between gap-3 border-t pt-3'>
                <Button
                  type='button'
                  size='sm'
                  variant='outline'
                  disabled={historyPage <= 1 || historyQuery.isFetching}
                  onClick={() =>
                    setHistoryPage((page) => Math.max(1, page - 1))
                  }
                >
                  {t('Previous')}
                </Button>
                <span className='text-muted-foreground text-xs tabular-nums'>
                  {t('Page')} {historyPage} {t('of')} {historyPageCount}
                </span>
                <Button
                  type='button'
                  size='sm'
                  variant='outline'
                  disabled={
                    historyPage >= historyPageCount || historyQuery.isFetching
                  }
                  onClick={() =>
                    setHistoryPage((page) =>
                      Math.min(historyPageCount, page + 1)
                    )
                  }
                >
                  {t('Next')}
                </Button>
              </div>
            )}
          </ScrollArea>
        </DialogContent>
      </Dialog>
    </>
  )
}

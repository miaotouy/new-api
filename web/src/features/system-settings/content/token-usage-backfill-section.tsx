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
import { Database, RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'

import {
  getCurrentTokenUsageBackfillTask,
  getTokenUsageBackfillTask,
  startTokenUsageBackfillTask,
} from '../api'
import { SettingsSection } from '../components/settings-section'
import type { TokenUsageBackfillTask } from '../types'

function isActive(task: TokenUsageBackfillTask | null) {
  return task?.status === 'pending' || task?.status === 'running'
}

export function TokenUsageBackfillSection() {
  const { t } = useTranslation()
  const [task, setTask] = useState<TokenUsageBackfillTask | null>(null)
  const [starting, setStarting] = useState(false)

  useEffect(() => {
    let cancelled = false
    void getCurrentTokenUsageBackfillTask()
      .then((res) => {
        if (!cancelled && res.success) setTask(res.data ?? null)
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [])

  const active = isActive(task)
  useEffect(() => {
    if (!active || !task?.task_id) return
    let cancelled = false
    const interval = window.setInterval(() => {
      void getTokenUsageBackfillTask(task.task_id)
        .then((res) => {
          if (!cancelled && res.success && res.data) setTask(res.data)
        })
        .catch(() => undefined)
    }, 1000)
    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [active, task?.task_id])

  const handleStart = async () => {
    if (active || starting) return
    setStarting(true)
    try {
      const res = await startTokenUsageBackfillTask()
      if (!res.success || !res.data) {
        toast.error(res.message || t('Failed to start token usage migration'))
        return
      }
      setTask(res.data)
      toast.success(t('Token usage migration started'))
    } catch {
      toast.error(t('Failed to start token usage migration'))
    } finally {
      setStarting(false)
    }
  }

  const progress = Math.min(100, Math.max(0, task?.state?.progress ?? 0))
  const processed = task?.state?.processed ?? 0
  const total = task?.state?.total ?? 0
  let statusLabel = t('No migration has been run')
  if (active) {
    statusLabel = t('Migration in progress')
  } else if (task?.status === 'succeeded') {
    statusLabel = t('Migration completed')
  } else if (task?.status === 'failed') {
    statusLabel = t('Migration failed')
  }

  return (
    <SettingsSection title={t('Token Usage Migration')}>
      <div className='space-y-4'>
        <Alert>
          <Database />
          <AlertDescription>
            {t(
              'Backfill token and cache-read metrics from consume logs for the last 30 days. Existing logs without cache fields are kept unchanged.'
            )}
          </AlertDescription>
        </Alert>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <div className='text-muted-foreground text-sm'>
            {statusLabel}
            {active && (
              <div className='mt-2 flex items-center gap-2 text-xs'>
                <Progress value={progress} className='w-40' />
                <span>{progress}%</span>
                {total > 0 && <span>{`${processed}/${total}`}</span>}
              </div>
            )}
            {task?.status === 'failed' && task.error && (
              <div className='text-destructive mt-1 text-xs'>{task.error}</div>
            )}
          </div>
          <Button onClick={handleStart} disabled={active || starting}>
            <RefreshCw
              data-icon='inline-start'
              className={starting ? 'animate-spin' : undefined}
            />
            {active ? t('Migration in progress') : t('Migrate last 30 days')}
          </Button>
        </div>
        {task?.status === 'succeeded' && task.result && (
          <div className='text-muted-foreground grid gap-2 text-xs sm:grid-cols-3'>
            <span>
              {t('Scanned logs')}: {task.result.scanned_logs}
            </span>
            <span>
              {t('Cache-bearing logs')}: {task.result.logs_with_cache_tokens}
            </span>
            <span>
              {t('Cached tokens')}: {task.result.cached_tokens}
            </span>
          </div>
        )}
      </div>
    </SettingsSection>
  )
}

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
  LOG_CATEGORY_LABELS,
  LOG_TYPES,
  MJ_STATUS_MAPPINGS,
  MJ_SUBMIT_RESULT_MAPPINGS,
  MJ_TASK_TYPE_MAPPINGS,
  TASK_ACTION_MAPPINGS,
  TASK_PLATFORM_MAPPINGS,
  TASK_STATUS_MAPPINGS,
} from '../constants'
import type {
  LogCategory,
  LogExportColumn,
  LogExportFilters,
  LogExportFormat,
  LogExportTask,
} from '../types'
import { buildApiParams, buildBaseParams } from './utils'

export const logExportFormatOptions: Array<{
  format: LogExportFormat
  label: string
}> = [
  { format: 'csv', label: 'Export as CSV' },
  { format: 'json', label: 'Export as JSON' },
  { format: 'md', label: 'Export as Markdown' },
]

export const exportColumnLabelKeys: Record<
  LogCategory,
  Record<string, string>
> = {
  common: {
    created_at: 'Time',
    channel: 'Channel',
    user: 'User',
    token_name: 'Token',
    model_name: 'Model',
    is_stream: 'Stream',
    prompt_tokens: 'Tokens',
    quota: 'Cost',
    use_time: 'Timing',
    content: 'Details',
  },
  drawing: {
    submit_time: 'Submit Time',
    channel_id: 'Channel',
    action: 'Type',
    mj_id: 'Task ID',
    duration: 'Duration',
    code: 'Submit Result',
    progress: 'Progress',
    prompt: 'Prompt',
  },
  task: {
    submit_time: 'Submit Time',
    channel_id: 'Channel',
    user: 'User',
    task_id: 'Task ID',
    duration: 'Duration',
    status: 'Status',
    progress: 'Progress',
    fail_reason: 'Details',
  },
}

type Translate = (key: string) => string

function addMappingLabels(
  target: Record<string, string>,
  prefix: string,
  mappings: Record<string, { label: string }>,
  translate: Translate
) {
  Object.entries(mappings).forEach(([value, config]) => {
    target[`${prefix}:${value}`] = translate(config.label)
  })
}

export function buildPresentationLabels(
  category: LogCategory,
  translate: Translate
): Record<string, string> {
  const labels: Record<string, string> = {
    yes: translate('Yes'),
    no: translate('No'),
    export_title: translate('Usage Logs Export'),
    category: translate('Category'),
    scope: translate('Scope'),
    generated_at: translate('Generated At'),
    row_count: translate('Row Count'),
    input_tokens: translate('Input Tokens'),
    output_tokens: translate('Output Tokens'),
    cache_read: translate('Cache Read'),
    cache_write: translate('Cache Write'),
    duration: translate('Duration'),
    first_token: translate('First token'),
    throughput: translate('Throughput'),
    'scope:all': translate('All'),
    'scope:self': translate('Only Mine'),
  }
  Object.entries(LOG_CATEGORY_LABELS).forEach(([value, label]) => {
    labels[`category:${value}`] = translate(label)
  })
  LOG_TYPES.forEach(({ value, label }) => {
    labels[`log_type:${value}`] = translate(label)
  })
  if (category === 'drawing') {
    addMappingLabels(labels, 'status', MJ_STATUS_MAPPINGS, translate)
    addMappingLabels(labels, 'action', MJ_TASK_TYPE_MAPPINGS, translate)
    addMappingLabels(labels, 'code', MJ_SUBMIT_RESULT_MAPPINGS, translate)
  }
  if (category === 'task') {
    addMappingLabels(labels, 'status', TASK_STATUS_MAPPINGS, translate)
    addMappingLabels(labels, 'action', TASK_ACTION_MAPPINGS, translate)
    addMappingLabels(labels, 'platform', TASK_PLATFORM_MAPPINGS, translate)
  }
  return labels
}

export function buildExportColumns(
  category: LogCategory,
  visibleColumnIDs: string[],
  translate: Translate
): LogExportColumn[] {
  const availableLabels = exportColumnLabelKeys[category]
  return visibleColumnIDs
    .filter((key) => availableLabels[key])
    .map((key) => ({ key, label: translate(availableLabels[key]) }))
}

export function buildExportFilters(
  logCategory: LogCategory,
  isAdmin: boolean,
  searchParams: Record<string, unknown>,
  columnFilters: Array<{ id: string; value: unknown }>
): LogExportFilters {
  if (logCategory === 'common') {
    const params = buildApiParams({
      page: 1,
      pageSize: 1000,
      searchParams,
      columnFilters,
      isAdmin,
    })
    const { p: _page, page_size: _pageSize, ...filters } = params
    return filters
  }

  const params = buildBaseParams({
    page: 1,
    pageSize: 1000,
    searchParams,
    useMilliseconds: logCategory === 'drawing',
  })
  const { p: _page, page_size: _pageSize, ...filters } = params
  if (logCategory === 'drawing' && searchParams.filter) {
    return { ...filters, mj_id: String(searchParams.filter) }
  }
  if (logCategory === 'task' && searchParams.filter) {
    return { ...filters, task_id: String(searchParams.filter) }
  }
  return filters
}

export function hasActiveLogExports(tasks: LogExportTask[]): boolean {
  return tasks.some(
    (task) => task.status === 'pending' || task.status === 'running'
  )
}

export function collectAutoDownloadTransitions(
  tasks: LogExportTask[],
  autoDownloadTaskIDs: Set<string>,
  handledTaskIDs: Set<string>
): { succeeded: LogExportTask[]; failed: LogExportTask[] } {
  const succeeded: LogExportTask[] = []
  const failed: LogExportTask[] = []
  for (const task of tasks) {
    if (
      !autoDownloadTaskIDs.has(task.task_id) ||
      handledTaskIDs.has(task.task_id)
    ) {
      continue
    }
    if (task.status === 'succeeded') {
      handledTaskIDs.add(task.task_id)
      succeeded.push(task)
    } else if (task.status === 'failed') {
      handledTaskIDs.add(task.task_id)
      failed.push(task)
    }
  }
  return { succeeded, failed }
}

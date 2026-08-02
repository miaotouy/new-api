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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { LogExportTask } from '../../types'
import {
  buildExportColumns,
  buildExportFilters,
  buildPresentationLabels,
  collectAutoDownloadTransitions,
  hasActiveLogExports,
  logExportFormatOptions,
} from '../export'

const translate = (key: string) => `translated:${key}`

function exportTask(
  taskId: string,
  status: LogExportTask['status']
): LogExportTask {
  return {
    id: 1,
    task_id: taskId,
    user_id: 1,
    category: 'common',
    format: 'csv',
    scope: 'self',
    status,
    progress: 0,
    processed_rows: 0,
    total_rows: 0,
    file_name: '',
    content_type: '',
    file_size: 0,
    stored_size: 0,
    chunk_count: 0,
    error: '',
    attempts: 0,
    snapshot_at: 0,
    created_at: 0,
    updated_at: 0,
    completed_at: 0,
    expires_at: 0,
  }
}

describe('usage log export request snapshots', () => {
  test('offers CSV, JSON, and Markdown exports', () => {
    assert.deepEqual(
      logExportFormatOptions.map((option) => option.format),
      ['csv', 'json', 'md']
    )
  })

  test('preserves visible column order and removes interactive columns', () => {
    assert.deepEqual(
      buildExportColumns(
        'drawing',
        ['prompt', 'image_url', 'submit_time', 'actions', 'mj_id'],
        translate
      ),
      [
        { key: 'prompt', label: 'translated:Prompt' },
        { key: 'submit_time', label: 'translated:Submit Time' },
        { key: 'mj_id', label: 'translated:Task ID' },
      ]
    )
  })

  test('binds common filters to the selected admin or self scope', () => {
    const search = {
      username: 'alice',
      channel: '12',
      model: 'gpt-5',
      startTime: 1_780_000_000_000,
      endTime: 1_780_003_600_000,
    }
    const admin = buildExportFilters('common', true, search, [])
    assert.equal(admin.username, 'alice')
    assert.equal(admin.channel, 12)
    assert.equal(admin.model_name, 'gpt-5')
    assert.equal(admin.start_timestamp, 1_780_000_000)

    const self = buildExportFilters('common', false, search, [])
    assert.equal(self.username, undefined)
    assert.equal(self.channel, undefined)
    assert.equal(self.model_name, 'gpt-5')
  })

  test('keeps drawing timestamps in milliseconds and task timestamps in seconds', () => {
    const search = {
      filter: 'task-123',
      startTime: 1_780_000_000_000,
      endTime: 1_780_003_600_000,
    }
    const drawing = buildExportFilters('drawing', false, search, [])
    assert.equal(drawing.start_timestamp, 1_780_000_000_000)
    assert.equal(drawing.mj_id, 'task-123')

    const task = buildExportFilters('task', false, search, [])
    assert.equal(task.start_timestamp, 1_780_000_000)
    assert.equal(task.task_id, 'task-123')
  })

  test('captures localized metadata and status labels', () => {
    const labels = buildPresentationLabels('task', translate)
    assert.equal(labels.export_title, 'translated:Usage Logs Export')
    assert.equal(labels['scope:self'], 'translated:Only Mine')
    assert.equal(labels['category:task'], 'translated:Task')
    assert.ok(labels['status:SUCCESS'])
    assert.ok(labels['platform:kling'])
  })
})

describe('usage log export polling transitions', () => {
  test('polls while active and auto-downloads each terminal task once', () => {
    const pending = exportTask('pending-task', 'pending')
    assert.equal(hasActiveLogExports([pending]), true)
    assert.equal(hasActiveLogExports([exportTask('done', 'succeeded')]), false)

    const auto = new Set(['success-task', 'failed-task'])
    const handled = new Set<string>()
    const tasks = [
      exportTask('success-task', 'succeeded'),
      exportTask('failed-task', 'failed'),
      exportTask('history-only', 'succeeded'),
    ]
    const first = collectAutoDownloadTransitions(tasks, auto, handled)
    assert.deepEqual(
      first.succeeded.map((task) => task.task_id),
      ['success-task']
    )
    assert.deepEqual(
      first.failed.map((task) => task.task_id),
      ['failed-task']
    )

    const second = collectAutoDownloadTransitions(tasks, auto, handled)
    assert.deepEqual(second, { succeeded: [], failed: [] })
  })
})

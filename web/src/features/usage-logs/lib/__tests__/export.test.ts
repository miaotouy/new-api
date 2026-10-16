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

import { createInstance } from 'i18next'
import { describe, expect, test } from 'vitest'

import type { UsageLog } from '../../data/schema'
import type { LogExportTask } from '../../types'
import {
  getLogCsvHeader,
  getLogCsvRow,
  toCsvLine,
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

const consumeLog: UsageLog = {
  id: 1,
  user_id: 7,
  created_at: new Date(2026, 9, 12, 8, 30, 5).getTime() / 1000,
  type: 2,
  content: '',
  username: 'alice',
  token_name: 'prod',
  model_name: 'gpt-4o-mini',
  quota: 250000,
  prompt_tokens: 2008,
  completion_tokens: 16,
  use_time: 3,
  is_stream: true,
  channel: 162,
  channel_name: 'azure',
  token_id: 5,
  group: 'default',
  ip: '203.0.113.9',
  other: JSON.stringify({
    cache_tokens: 1906,
    cache_creation_tokens_5m: 10,
    cache_creation_tokens_1h: 5,
  }),
  request_id: 'req-1',
  upstream_request_id: '',
}

async function chineseTranslator() {
  const i18n = createInstance()
  await i18n.init({
    lng: 'zh',
    resources: {
      zh: {
        translation: {
          Time: '时间',
          User: '用户',
          Channel: '渠道',
          Consume: '消费',
          'Top-up': '充值',
          Yes: '是',
          'Recharged {{amount}}': '充值 {{amount}}',
        },
      },
    },
  })
  return i18n.t
}

describe('usage log CSV export', () => {
  test('header in the user view has no user or channel column', async () => {
    const t = await chineseTranslator()

    const header = getLogCsvHeader(false, t)

    expect(header[0]).toBe('时间')
    expect(header).not.toContain('用户')
    expect(header).not.toContain('渠道')
  })

  test('administrator row shows the user and channel next to the matching header', async () => {
    const t = await chineseTranslator()

    const header = getLogCsvHeader(true, t)
    const row = getLogCsvRow(consumeLog, true, t)

    expect(row).toHaveLength(header.length)
    expect(row[header.indexOf('用户')]).toBe('alice')
    expect(row[header.indexOf('渠道')]).toBe('#162 azure')
  })

  test('consume log row matches the log table values', async () => {
    const t = await chineseTranslator()

    expect(getLogCsvRow(consumeLog, false, t)).toEqual([
      '2026-10-12 08:30:05',
      '消费',
      'prod',
      'default',
      'gpt-4o-mini',
      '是',
      '2008',
      '16',
      '1906',
      '15',
      '$0.5',
      '3s',
      '203.0.113.9',
      'req-1',
      '',
    ])
  })

  test('top-up log row leaves usage cells empty and shows the translated content', async () => {
    const t = await chineseTranslator()
    const topup: UsageLog = {
      ...consumeLog,
      type: 1,
      content: 'Recharged $5',
      other: JSON.stringify({
        content_parts: [
          { key: 'Recharged {{amount}}', params: { amount: '$5' } },
        ],
      }),
    }

    const row = getLogCsvRow(topup, false, t)

    expect(row.slice(1, 2)).toEqual(['充值'])
    expect(row.slice(5, 12)).toEqual(['', '', '', '', '', '', ''])
    expect(row.at(-1)).toBe('充值 $5')
  })

  test('cells are quoted and text that starts a formula stays text', () => {
    expect(
      toCsvLine(['=HYPERLINK("x")', '-12.5', '+cmd', 'a"b', 'two\nlines'])
    ).toBe(`"'=HYPERLINK(""x"")","-12.5","'+cmd","a""b","two\nlines"\r\n`)
  })
})

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

import { Blob as NodeBlob, Buffer } from 'node:buffer'

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import {
  buildFlowExportDataset,
  downloadDashboardExport,
  serializeDashboardExport,
  type DashboardExportDataset,
} from '../../../lib/export'
import { buildDashboardFlowData } from '../../../lib/flow'
import { DashboardExportButton } from '../dashboard-export-button'

const datasets: DashboardExportDataset[] = [
  {
    name: 'Requests',
    columns: [
      { key: 'model', label: 'Model' },
      { key: 'requests', label: 'Requests' },
    ],
    rows: [['模型,"A"\nB', 0]],
  },
  {
    name: 'Tokens',
    columns: [
      { key: 'model', label: 'Model' },
      { key: 'tokens', label: 'Tokens' },
    ],
    rows: [['gpt-4.1', 1234]],
  },
]

let blobs: NodeBlob[]
let filenames: string[]
const revokeObjectURL = vi.fn()

beforeEach(() => {
  blobs = []
  filenames = []
  vi.stubGlobal('Blob', NodeBlob)
  vi.stubGlobal(
    'URL',
    class extends URL {
      static createObjectURL(blob: Blob | MediaSource) {
        if (!(blob instanceof NodeBlob)) {
          throw new Error('Expected an export Blob')
        }
        blobs.push(blob)
        return 'blob:dashboard-export'
      }
      static revokeObjectURL = revokeObjectURL
    }
  )
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(
    function (this: HTMLAnchorElement) {
      filenames.push(this.download)
    }
  )
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('dashboard export files', () => {
  test('CSV combines datasets with matching columns, escapes text, and preserves zero values', () => {
    expect(serializeDashboardExport(datasets, 'csv', 'Dataset')).toBe(
      '\uFEFF"Dataset","Model","Requests","Tokens"\r\n' +
        '"Requests","模型,""A""\nB","0",""\r\n' +
        '"Tokens","gpt-4.1","","1234"\r\n'
    )
  })

  test.each(['=SUM(1,2)', '+cmd', '-cmd', '@cmd', '  =1', '\t1', '\r1', '\n1'])(
    'CSV treats spreadsheet formula text %j as a literal',
    (value) => {
      const data = [
        {
          name: 'Users',
          columns: [{ key: 'user', label: 'User' }],
          rows: [[value]],
        },
      ]
      expect(serializeDashboardExport(data, 'csv', 'Dataset')).toContain(
        `"'${value}"`
      )
    }
  )

  test('JSON preserves numeric precision, booleans, and unescaped original text', () => {
    const data = [
      {
        name: 'Usage',
        columns: [{ key: 'value', label: 'Value' }],
        rows: [[0], [0.000001], [false], ['=1'], [null]],
      },
    ]
    expect(
      JSON.parse(serializeDashboardExport(data, 'json', 'Dataset'))
    ).toEqual({ datasets: data })
  })

  test('download assigns the requested filename and releases its object URL after activation', async () => {
    vi.useFakeTimers()
    downloadDashboardExport(datasets, 'csv', 'Dataset', 'dashboard-models.csv')
    expect(filenames).toEqual(['dashboard-models.csv'])
    expect(blobs[0].type).toBe('text/csv;charset=utf-8')
    expect(Buffer.from(await blobs[0].arrayBuffer()).toString('utf8')).toBe(
      serializeDashboardExport(datasets, 'csv', 'Dataset')
    )
    expect(revokeObjectURL).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1000)
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:dashboard-export')
  })
})

describe('dashboard export action', () => {
  test('exports the current filtered snapshot after the displayed data changes', async () => {
    const user = userEvent.setup()
    const view = render(
      <DashboardExportButton
        filename='dashboard-users'
        getDatasets={() => datasets}
      />
    )
    const current = [{ ...datasets[0], rows: [['selected-user', 8]] }]
    view.rerender(
      <DashboardExportButton
        filename='dashboard-users'
        getDatasets={() => current}
      />
    )
    await user.click(screen.getByRole('button', { name: 'Export' }))
    await user.click(
      await screen.findByRole('menuitem', { name: 'Export as CSV' })
    )
    expect(Buffer.from(await blobs[0].arrayBuffer()).toString('utf8')).toBe(
      serializeDashboardExport(current, 'csv', 'Dataset')
    )
    expect(filenames[0]).toMatch(
      /^dashboard-users-\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}\.csv$/
    )
  })

  test('disables the action while the current data is unavailable', () => {
    render(
      <DashboardExportButton
        filename='dashboard-flow'
        disabled
        getDatasets={() => datasets}
      />
    )
    expect(screen.getByRole('button', { name: 'Export' })).toBeDisabled()
  })

  test('reports a download failure with an actionable message', async () => {
    const user = userEvent.setup()
    vi.spyOn(URL, 'createObjectURL').mockImplementation(() => {
      throw new Error('Download failed')
    })
    const errorToast = vi.spyOn(toast, 'error')
    render(
      <DashboardExportButton
        filename='dashboard-models'
        getDatasets={() => datasets}
      />
    )
    await user.click(screen.getByRole('button', { name: 'Export' }))
    await user.click(
      await screen.findByRole('menuitem', { name: 'Export as JSON' })
    )
    expect(errorToast).toHaveBeenCalledWith(
      'Failed to export data. Please try again.'
    )
  })
})

test('flow export keeps the selected users and rendered masked labels', () => {
  const graph = buildDashboardFlowData(
    [
      {
        user_id: 1,
        username: 'alice',
        model_name: 'gpt-4.1',
        channel_id: 101,
        channel_name: 'east',
        quota: 0,
        token_used: 40,
        count: 2,
      },
      {
        user_id: 2,
        username: 'bob',
        model_name: 'other-model',
        channel_id: 102,
        channel_name: 'west',
        quota: 0,
        token_used: 90,
        count: 3,
      },
    ],
    'tokens',
    {
      role: 'root',
      selectedUsers: ['user:1'],
      visibleStages: ['user', 'model', 'channel'],
      maskSensitive: true,
    }
  ).flow
  const dataset = buildFlowExportDataset(graph, 'Flow', i18next.t)
  expect(dataset.rows).toHaveLength(2)
  expect(dataset.rows).toEqual(
    expect.arrayContaining([
      ['••••', 'gpt-4.1', 2, 0, 40],
      ['gpt-4.1', '••••', 2, 0, 40],
    ])
  )
  expect(
    JSON.parse(serializeDashboardExport([dataset], 'json', 'Dataset'))
      .datasets[0].rows
  ).toEqual(dataset.rows)
})

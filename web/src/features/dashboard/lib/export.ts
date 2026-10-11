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

import type { TFunction } from 'i18next'

import { getCurrencyLabel } from '@/lib/currency'
import { quotaUnitsToDollars } from '@/lib/format'

import type { DashboardFlowGraph } from '../types'

export type DashboardExportValue = string | number | boolean | null | undefined

export interface DashboardExportDataset {
  name: string
  columns: { key: string; label: string }[]
  rows: DashboardExportValue[][]
}

export type DashboardExportFormat = 'csv' | 'json'

function escapeCsvCell(value: DashboardExportValue): string {
  let text = value == null ? '' : String(value)
  // Treat spreadsheet formulas as literal text, while preserving numeric values.
  if (typeof value === 'string' && /^\s*[=+\-@\t\r\n]/.test(text)) {
    text = `'${text}`
  }
  return `"${text.replaceAll('"', '""')}"`
}

export function serializeDashboardExport(
  datasets: DashboardExportDataset[],
  format: DashboardExportFormat,
  datasetLabel: string
): string {
  if (format === 'json') {
    return `${JSON.stringify({ datasets }, null, 2)}\n`
  }

  const columns = new Map<string, string>()
  for (const dataset of datasets) {
    for (const column of dataset.columns) {
      if (!columns.has(column.key)) columns.set(column.key, column.label)
    }
  }

  const lines = [
    [datasetLabel, ...columns.values()].map(escapeCsvCell).join(','),
  ]
  for (const dataset of datasets) {
    for (const row of dataset.rows) {
      const values = new Map(
        dataset.columns.map((column, index) => [column.key, row[index]])
      )
      lines.push(
        [dataset.name, ...Array.from(columns.keys(), (key) => values.get(key))]
          .map(escapeCsvCell)
          .join(',')
      )
    }
  }
  return `\uFEFF${lines.join('\r\n')}\r\n`
}

export function downloadDashboardExport(
  datasets: DashboardExportDataset[],
  format: DashboardExportFormat,
  datasetLabel: string,
  filename: string
): void {
  const contents = serializeDashboardExport(datasets, format, datasetLabel)
  const type =
    format === 'csv'
      ? 'text/csv;charset=utf-8'
      : 'application/json;charset=utf-8'
  const url = URL.createObjectURL(new Blob([contents], { type }))
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  try {
    document.body.append(anchor)
    anchor.click()
  } finally {
    anchor.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
}

// Export the rendered graph so node filters, overflow aggregation, and masking
// remain identical to the current view. Raw node identifiers stay in the graph.
export function buildFlowExportDataset(
  graph: DashboardFlowGraph,
  name: string,
  t: TFunction
): DashboardExportDataset {
  return {
    name,
    columns: [
      { key: 'source', label: t('Source') },
      { key: 'target', label: t('Target') },
      { key: 'requests', label: t('Requests') },
      { key: 'quota', label: `${t('Quota')} (${getCurrencyLabel()})` },
      { key: 'tokens', label: t('Tokens') },
    ],
    rows: graph.links.map((link) => [
      link.sourceLabel,
      link.targetLabel,
      link.requests,
      quotaUnitsToDollars(link.quota),
      link.tokens,
    ]),
  }
}

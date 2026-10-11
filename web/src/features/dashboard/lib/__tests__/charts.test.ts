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
import { describe, expect, test } from 'vitest'

import { formatChartTime, type TimeGranularity } from '@/lib/time'

import {
  DASHBOARD_VALUE_AXIS_WIDTH,
  MAX_CHART_TREND_POINTS,
} from '../../constants'
import type { QuotaDataItem } from '../../types'
import {
  buildChartTimePoints,
  processChartData,
  processUserChartData,
} from '../charts'

const HOUR = 3600
const DAY = 86400
const hourStart = (timestamp: number) => timestamp - (timestamp % HOUR)
const BASE = hourStart(1_700_000_000)

describe('buildChartTimePoints', () => {
  test('pads sparse hours to the shared trend-point count ending at the latest bucket', () => {
    const points = buildChartTimePoints(
      [
        { created_at: BASE },
        { created_at: BASE + HOUR },
        { created_at: BASE + 2 * HOUR },
      ],
      'hour'
    )

    expect(points).toHaveLength(MAX_CHART_TREND_POINTS)
    expect(points.at(-1)).toBe(formatChartTime(BASE + 2 * HOUR, 'hour'))
    expect(new Set(points).size).toBe(MAX_CHART_TREND_POINTS)
  })

  test('keeps all unique buckets when they already meet the trend-point count', () => {
    const items = Array.from(
      { length: MAX_CHART_TREND_POINTS + 2 },
      (_, i) => ({ created_at: BASE + i * HOUR })
    )

    const points = buildChartTimePoints(items, 'hour')

    expect(points).toHaveLength(MAX_CHART_TREND_POINTS + 2)
    expect(points).toEqual([...points].sort())
  })

  test('pads by day interval for day granularity', () => {
    const points = buildChartTimePoints(
      [{ created_at: BASE }, { created_at: BASE + DAY }],
      'day'
    )

    expect(points).toHaveLength(MAX_CHART_TREND_POINTS)
    expect(points.at(-1)).toBe(formatChartTime(BASE + DAY, 'day'))
  })

  test('ignores empty and non-positive timestamps', () => {
    expect(buildChartTimePoints([], 'hour')).toEqual([])
    expect(buildChartTimePoints([{ created_at: 0 }], 'hour')).toEqual([])
  })
})

describe('processChartData shared time axis', () => {
  const timePoints = Array.from({ length: MAX_CHART_TREND_POINTS }, (_, i) =>
    formatChartTime(BASE + i * HOUR, 'hour')
  )
  const data: QuotaDataItem[] = [
    { model_name: 'gpt-4.1', created_at: BASE, quota: 100, count: 1 },
  ]

  const specTimes = (spec: Record<string, unknown>) => {
    const values = (spec.data as Array<{ values: Array<{ Time: string }> }>)[0]
      .values
    return [...new Set(values.map((value) => value.Time))]
  }

  test('renders every time-based spec against the provided time points', () => {
    const result = processChartData(
      data,
      'hour',
      undefined,
      0,
      'requests',
      timePoints
    )

    expect(specTimes(result.spec_line)).toEqual(timePoints)
    expect(specTimes(result.spec_area)).toEqual(timePoints)
    expect(specTimes(result.spec_model_line)).toEqual(timePoints)
  })

  test('reserves the same value-axis width so chart columns align', () => {
    const result = processChartData(
      data,
      'hour',
      undefined,
      0,
      'requests',
      timePoints
    )

    const valueAxisWidth = (spec: Record<string, unknown>) => {
      const axes = spec.axes as Array<{ orient: string; width?: number }>
      return axes.find((axis) => axis.orient === 'left')?.width
    }

    expect(valueAxisWidth(result.spec_line)).toBe(DASHBOARD_VALUE_AXIS_WIDTH)
    expect(valueAxisWidth(result.spec_area)).toBe(DASHBOARD_VALUE_AXIS_WIDTH)
    expect(valueAxisWidth(result.spec_model_line)).toBe(
      DASHBOARD_VALUE_AXIS_WIDTH
    )
  })
})

const cases: Array<{
  granularity: TimeGranularity
  start: string
  interval: number
  labels: string[]
}> = [
  {
    granularity: 'hour',
    start: '2025-12-31T20:00:00',
    interval: 3600,
    labels: [
      '12-31 20:00',
      '12-31 21:00',
      '12-31 22:00',
      '12-31 23:00',
      '01-01 00:00',
      '01-01 01:00',
      '01-01 02:00',
      '01-01 03:00',
    ],
  },
  {
    granularity: 'day',
    start: '2025-12-27T12:00:00',
    interval: 86400,
    labels: [
      '12-27',
      '12-28',
      '12-29',
      '12-30',
      '12-31',
      '01-01',
      '01-02',
      '01-03',
    ],
  },
  {
    granularity: 'week',
    start: '2025-11-29T12:00:00',
    interval: 604800,
    labels: [
      '11-29 - 12-05',
      '12-06 - 12-12',
      '12-13 - 12-19',
      '12-20 - 12-26',
      '12-27 - 01-02',
      '01-03 - 01-09',
      '01-10 - 01-16',
      '01-17 - 01-23',
    ],
  },
]

function unorderedUsage(start: string, interval: number): QuotaDataItem[] {
  const first = new Date(start).getTime() / 1000
  const rows = Array.from({ length: 8 }, (_, i) => ({
    created_at: first + i * interval,
    model_name: 'model-a',
    username: 'alice',
    quota: (i + 1) * 500000,
    count: i + 1,
  }))
  // Same hour, or a second hourly record within the same day/week label.
  const duplicate = {
    ...rows[0],
    created_at: first + (interval === 3600 ? 0 : 3600),
    quota: 5000000,
    count: 10,
  }
  return [
    rows[7],
    rows[2],
    duplicate,
    rows[5],
    rows[0],
    rows[6],
    rows[1],
    rows[4],
    rows[3],
  ]
}

describe.each(cases)('dashboard $granularity chart chronology', (scenario) => {
  test('orders model buckets across New Year while aggregating repeated labels', () => {
    const data = unorderedUsage(scenario.start, scenario.interval)
    const original = structuredClone(data)
    const result = processChartData(
      data,
      scenario.granularity,
      undefined,
      0,
      'quota'
    )

    for (const key of ['spec_line', 'spec_area', 'spec_model_line'] as const) {
      const values: Array<{ Time: string; rawQuota: number; Count: number }> =
        result[key].data[0].values
      expect(values.map((row) => row.Time)).toEqual(scenario.labels)
      expect(
        values.map(
          (row) =>
            (key === 'spec_model_line' ? row.Count : row.rawQuota) / 500000
        )
      ).toEqual([11, 2, 3, 4, 5, 6, 7, 8])
    }
    expect(data).toEqual(original)
  })

  test('orders user buckets across New Year while aggregating repeated labels', () => {
    const data = unorderedUsage(scenario.start, scenario.interval)
    const original = structuredClone(data)
    const result = processUserChartData(data, scenario.granularity)
    const values: Array<{ Time: string; rawQuota: number }> =
      result.spec_user_trend.data[0].values

    expect(values.map((row) => row.Time)).toEqual(scenario.labels)
    expect(values.map((row) => row.rawQuota / 500000)).toEqual([
      11, 2, 3, 4, 5, 6, 7, 8,
    ])
    expect(data).toEqual(original)
  })

  test('keeps the existing seven padded model buckets in chronological order', () => {
    const data = [unorderedUsage(scenario.start, scenario.interval)[0]]
    const result = processChartData(
      data,
      scenario.granularity,
      undefined,
      0,
      'quota'
    )

    for (const key of ['spec_line', 'spec_area', 'spec_model_line'] as const) {
      const values: Array<{ Time: string; rawQuota: number; Count: number }> =
        result[key].data[0].values
      expect(values.map((row) => row.Time)).toEqual(scenario.labels.slice(1))
      expect(
        values.map(
          (row) =>
            (key === 'spec_model_line' ? row.Count : row.rawQuota) / 500000
        )
      ).toEqual([0, 0, 0, 0, 0, 0, 8])
    }
  })
})

describe('user analytics export data', () => {
  test('ranking and trend exports share the displayed metric, Top users, and time buckets', () => {
    const data: QuotaDataItem[] = [
      { username: 'alice', created_at: BASE, quota: 500000, token_used: 20 },
      {
        username: 'alice',
        created_at: BASE + HOUR,
        quota: 500000,
        token_used: 30,
      },
      { username: 'bob', created_at: BASE, quota: 2000000, token_used: 10 },
    ]
    const tokens = processUserChartData(data, 'hour', undefined, 1, 'tokens')
    expect(tokens.rankData).toEqual([
      { User: 'alice', rawQuota: 50, Usage: 50 },
    ])
    expect(tokens.trendData).toEqual([
      {
        Time: formatChartTime(BASE, 'hour'),
        User: 'alice',
        rawQuota: 20,
        Usage: 20,
      },
      {
        Time: formatChartTime(BASE + HOUR, 'hour'),
        User: 'alice',
        rawQuota: 30,
        Usage: 30,
      },
    ])
    expect(tokens.rankData).toEqual(tokens.spec_user_rank.data[0].values)
    expect(tokens.trendData).toEqual(tokens.spec_user_trend.data[0].values)

    const quota = processUserChartData(data, 'day', undefined, 1, 'quota')
    expect(quota.rankData.map((row) => row.User)).toEqual(['bob'])
    expect(quota.rankData).toEqual(quota.spec_user_rank.data[0].values)
    expect(quota.trendData).toEqual(quota.spec_user_trend.data[0].values)
  })
})

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

import { formatChartTime } from '@/lib/time'

import {
  DASHBOARD_VALUE_AXIS_WIDTH,
  MAX_CHART_TREND_POINTS,
} from '../../constants'
import type { QuotaDataItem } from '../../types'
import { buildChartTimePoints, processChartData } from '../charts'

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

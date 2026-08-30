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
import { VChart } from '@visactor/react-vchart'
import { ChartSpline } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { IconBadge } from '@/components/ui/icon-badge'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import { useTheme } from '@/context/theme-provider'
import { getDashboardChartColors } from '@/features/dashboard/lib/charts'
import type { TokenUsageDataItem } from '@/features/dashboard/types'
import dayjs from '@/lib/dayjs'
import { useThemeRadiusPx } from '@/lib/theme-radius'
import { formatChartTime, type TimeGranularity } from '@/lib/time'
import { VCHART_OPTION } from '@/lib/vchart'

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

type TokenUsageChartProps = {
  data: TokenUsageDataItem[]
  loading?: boolean
  timeGranularity: TimeGranularity
}

type TokenPoint = {
  Timestamp: number
  Time: string
  Metric: string
  Tokens: number
  InputTokens: number
  CachedTokens: number
}

function buildTokenChartData(
  data: TokenUsageDataItem[],
  timeGranularity: TimeGranularity,
  t: (key: string) => string,
  radius: number
) {
  const inputLabel = t('Input Tokens')
  const outputLabel = t('Output Tokens')
  const cachedLabel = t('Cached Tokens')
  const buckets = new Map<
    number,
    { input: number; output: number; cached: number }
  >()

  for (const item of data) {
    const timestamp = Number(item.created_at) || 0
    const date = dayjs(timestamp * 1000)
    let bucketTimestamp = date.startOf('hour').unix()
    if (timeGranularity === 'day') {
      bucketTimestamp = date.startOf('day').unix()
    } else if (timeGranularity === 'week') {
      bucketTimestamp = date.startOf('week').unix()
    }
    const bucket = buckets.get(bucketTimestamp) ?? {
      input: 0,
      output: 0,
      cached: 0,
    }
    bucket.input += Number(item.input_tokens) || 0
    bucket.output += Number(item.output_tokens) || 0
    bucket.cached += Number(item.cached_tokens) || 0
    buckets.set(bucketTimestamp, bucket)
  }

  const values: TokenPoint[] = []
  let totalTokens = 0
  let totalCachedTokens = 0
  for (const [bucketTimestamp, bucket] of buckets) {
    const time = formatChartTime(bucketTimestamp, timeGranularity)
    values.push(
      {
        Timestamp: bucketTimestamp,
        Time: time,
        Metric: inputLabel,
        Tokens: bucket.input,
        InputTokens: bucket.input,
        CachedTokens: bucket.cached,
      },
      {
        Timestamp: bucketTimestamp,
        Time: time,
        Metric: outputLabel,
        Tokens: bucket.output,
        InputTokens: bucket.input,
        CachedTokens: bucket.cached,
      },
      {
        Timestamp: bucketTimestamp,
        Time: time,
        Metric: cachedLabel,
        Tokens: bucket.cached,
        InputTokens: bucket.input,
        CachedTokens: bucket.cached,
      }
    )
    totalTokens += bucket.input + bucket.output
    totalCachedTokens += bucket.cached
  }
  values.sort((a, b) => a.Timestamp - b.Timestamp)

  const format = (value: number) =>
    Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value)
  const colors = getDashboardChartColors(3)

  return {
    totalTokens,
    totalCachedTokens,
    spec: {
      type: 'line',
      data: [{ id: 'tokenUsage', values }],
      xField: 'Time',
      yField: 'Tokens',
      seriesField: 'Metric',
      point: { visible: true, style: { size: 3 } },
      line: { style: { lineWidth: 2, cornerRadius: radius } },
      legends: { visible: true, selectMode: 'single' },
      color: colors,
      axes: [
        { orient: 'bottom', type: 'band' },
        {
          orient: 'left',
          type: 'linear',
          label: { formatMethod: (value: number) => format(value) },
        },
      ],
      tooltip: {
        dimension: {
          content: [
            {
              key: (datum: Record<string, unknown>) => datum?.Metric,
              value: (datum: Record<string, unknown>) =>
                Number(datum?.Tokens) || 0,
            },
          ],
          updateContent: (
            items: Array<{
              key: string
              value: string | number
              datum?: Record<string, unknown>
            }>
          ) => {
            const input = Number(items[0]?.datum?.InputTokens) || 0
            const cached = Number(items[0]?.datum?.CachedTokens) || 0
            const rate =
              input > 0
                ? Math.min(100, Math.max(0, (cached / input) * 100))
                : null
            return [
              ...items.map((item) => ({
                ...item,
                value: format(Number(item.value) || 0),
              })),
              {
                key: t('Cache Hit Rate'),
                value: rate === null ? '--' : `${rate.toFixed(1)}%`,
              },
            ]
          },
        },
      },
      background: { fill: 'transparent' },
      animation: true,
    },
  }
}

export function TokenUsageChart({
  data,
  loading,
  timeGranularity,
}: TokenUsageChartProps) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const { customization } = useThemeCustomization()
  const radius = useThemeRadiusPx(
    '--radius-md',
    `${customization.preset}:${customization.radius}`
  )
  const [themeReady, setThemeReady] = useState(false)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)

  useEffect(() => {
    const updateTheme = async () => {
      setThemeReady(false)
      themeManagerPromise ??= import('@visactor/vchart').then(
        (module) => module.ThemeManager
      )
      const ThemeManager = await themeManagerPromise
      themeManagerRef.current = ThemeManager
      ThemeManager.setCurrentTheme(resolvedTheme === 'dark' ? 'dark' : 'light')
      setThemeReady(true)
    }
    void updateTheme()
  }, [resolvedTheme])

  const chartData = useMemo(
    () =>
      buildTokenChartData(loading ? [] : data, timeGranularity, t, radius ?? 0),
    [data, loading, radius, t, timeGranularity]
  )
  const chartKey = [
    loading ? 'loading' : 'ready',
    data.length,
    timeGranularity,
    resolvedTheme,
    customization.preset,
  ].join('-')
  const numberFormat = Intl.NumberFormat(undefined, {
    maximumFractionDigits: 0,
  })

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex w-full flex-col gap-1.5 border-b px-3 py-2 sm:gap-3 sm:px-5 sm:py-3 lg:flex-row lg:items-center lg:justify-between'>
        <div className='flex items-center gap-2'>
          <IconBadge tone='chart-4' size='sm'>
            <ChartSpline />
          </IconBadge>
          <div className='text-sm font-semibold'>{t('Token Usage')}</div>
          <span className='text-muted-foreground text-xs'>
            {t('Total:')} {numberFormat.format(chartData.totalTokens)}
          </span>
        </div>
        <div className='text-muted-foreground text-xs'>
          {t('Cached:')} {numberFormat.format(chartData.totalCachedTokens)}
        </div>
      </div>
      <div className='h-[300px] p-1.5 sm:h-96 sm:p-2'>
        {themeReady && (
          <VChart
            key={chartKey}
            spec={{
              ...chartData.spec,
              theme: resolvedTheme === 'dark' ? 'dark' : 'light',
              background: 'transparent',
            }}
            option={VCHART_OPTION}
          />
        )}
      </div>
    </div>
  )
}

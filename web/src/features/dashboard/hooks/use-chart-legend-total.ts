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
import { useCallback, useMemo, useState } from 'react'

type ChartTotalSpec = {
  seriesField?: string
  categoryField?: string
  yField?: string
  valueField?: string
  data?: Array<{ values: Array<Record<string, unknown>> }>
}

type LegendSelectionEvent = {
  model: { getSelectedData: () => Array<string | number> }
}

export function useChartLegendTotal(spec: ChartTotalSpec, chartKey: string) {
  const [selection, setSelection] = useState<{
    chartKey: string
    values: Array<string | number>
  } | null>(null)

  if (selection && selection.chartKey !== chartKey) setSelection(null)

  const onLegendSelectedDataChange = useCallback(
    (event: LegendSelectionEvent) => {
      setSelection({ chartKey, values: [...event.model.getSelectedData()] })
    },
    [chartKey]
  )

  const selectedTotal = useMemo(() => {
    if (!selection || selection.chartKey !== chartKey) return undefined
    const seriesField = spec.seriesField ?? spec.categoryField
    const valueField = spec.valueField ?? spec.yField
    if (!seriesField || !valueField) return undefined

    const totals = new Map<string | number, number>()
    for (const dataset of spec.data ?? []) {
      for (const datum of dataset.values) {
        const series = datum[seriesField] as string | number
        // Distribution charts round plotted currency values; sum raw quota.
        const value = Number(datum.rawQuota ?? datum[valueField]) || 0
        totals.set(series, (totals.get(series) ?? 0) + value)
      }
    }
    const selected = new Set(selection.values)
    // Keep the chart's original total semantics when all series are visible.
    if ([...totals.keys()].every((series) => selected.has(series))) {
      return undefined
    }
    let total = 0
    for (const series of selected) total += totals.get(series) ?? 0
    return total
  }, [chartKey, selection, spec])

  return { selectedTotal, onLegendSelectedDataChange }
}

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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { QuotaDataItem, TokenUsageDataItem } from '../../../types'
import { ConsumptionDistributionChart } from '../consumption-distribution-chart'
import { ModelCharts } from '../model-charts'
import { TokenUsageChart } from '../token-usage-chart'

// Canvas rendering is the boundary: expose its legend selections as buttons.
vi.mock('@visactor/react-vchart', async () => {
  const { useState } = await import('react')
  return {
    VChart: (props: {
      spec: {
        seriesField?: string
        categoryField?: string
        legends: { selectMode?: string }
        data: Array<{ values: Array<Record<string, unknown>> }>
      }
      onLegendSelectedDataChange: (event: {
        model: { getSelectedData: () => string[] }
      }) => void
    }) => {
      const field = props.spec.seriesField ?? props.spec.categoryField ?? ''
      const series = [
        ...new Set(
          props.spec.data.flatMap((dataset) =>
            dataset.values.map((datum) => String(datum[field]))
          )
        ),
      ]
      const [selected, setSelected] = useState(series)
      return (
        <div role='group' aria-label='Chart legend'>
          {series.map((name) => (
            <button
              key={name}
              type='button'
              aria-pressed={selected.includes(name)}
              onClick={() => {
                let next: string[]
                if (props.spec.legends.selectMode === 'single') {
                  next =
                    selected.length === 1 && selected[0] === name
                      ? series
                      : [name]
                } else if (selected.includes(name)) {
                  next = selected.filter((item) => item !== name)
                } else {
                  next = [...selected, name]
                }
                setSelected(next)
                props.onLegendSelectedDataChange({
                  model: { getSelectedData: () => next },
                })
              }}
            >
              {name}
            </button>
          ))}
        </div>
      )
    },
  }
})

const data: QuotaDataItem[] = [
  {
    created_at: 1_700_000_000,
    model_name: 'model-a',
    count: 2,
    token_used: 100,
    quota: 250_001,
  },
  {
    created_at: 1_700_003_600,
    model_name: 'model-a',
    count: 3,
    token_used: 200,
    quota: 249_999,
  },
  {
    created_at: 1_700_000_000,
    model_name: 'model-b',
    count: 7,
    token_used: 700,
    quota: 1_000_000,
  },
]

const originalNamespaceSeparator = i18next.options.nsSeparator
beforeEach(() => {
  localStorage.clear()
  i18next.options.nsSeparator = false
})
afterEach(() => {
  i18next.options.nsSeparator = originalNamespaceSeparator
})

describe('dashboard legend totals', () => {
  it('updates input, output and cached token totals and restores the full total', async () => {
    const user = userEvent.setup()
    const tokens: TokenUsageDataItem[] = [
      {
        created_at: 1_700_000_000,
        input_tokens: 300,
        output_tokens: 20,
        cached_tokens: 200,
        token_used: 320,
      },
      {
        created_at: 1_700_003_600,
        input_tokens: 100,
        output_tokens: 10,
        cached_tokens: 50,
        token_used: 110,
      },
    ]
    const view = render(
      <TokenUsageChart data={tokens} timeGranularity='hour' />
    )
    expect(screen.getByText('Total: 430')).toBeVisible()
    await user.click(
      await screen.findByRole('button', { name: 'Input Tokens' })
    )
    expect(screen.getByText('Total: 400')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Output Tokens' }))
    expect(screen.getByText('Total: 30')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Cached Tokens' }))
    expect(screen.getByText('Total: 250')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Cached Tokens' }))
    expect(screen.getByText('Total: 430')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Input Tokens' }))
    await user.click(screen.getByRole('button', { name: 'Reset' }))
    expect(screen.getByText('Total: 430')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Input Tokens' }))
    view.rerender(<TokenUsageChart data={tokens} timeGranularity='day' />)
    expect(screen.getByText('Total: 430')).toBeVisible()
  })

  it.each(['bar', 'area'] as const)(
    'updates %s distribution totals using raw quota and the selected metric',
    async (chartType) => {
      const user = userEvent.setup()
      render(
        <ConsumptionDistributionChart
          data={data}
          defaultChartType={chartType}
          timeGranularity='hour'
        />
      )
      expect(screen.getByText('Total: $3.00')).toBeVisible()
      await user.click(await screen.findByRole('button', { name: 'model-a' }))
      expect(screen.getByText('Total: $1.00')).toBeVisible()
      await user.click(screen.getByRole('button', { name: 'By tokens' }))
      expect(screen.getByText('Total: 1,000')).toBeVisible()
      await user.click(screen.getByRole('button', { name: 'model-a' }))
      expect(screen.getByText('Total: 300')).toBeVisible()
      await user.click(screen.getByRole('button', { name: 'Reset' }))
      expect(screen.getByText('Total: 1,000')).toBeVisible()
      await user.click(screen.getByRole('button', { name: 'By quota' }))
      expect(screen.getByText('Total: $3.00')).toBeVisible()
    }
  )

  it.each(['trend', 'proportion', 'top'] as const)(
    'updates model %s totals and restores all selections',
    async (tab) => {
      const user = userEvent.setup()
      render(
        <ModelCharts data={data} defaultChartTab={tab} timeGranularity='hour' />
      )
      expect(screen.getByText('Total: 12')).toBeVisible()
      await user.click(
        await screen.findByRole('button', {
          name: tab === 'proportion' ? 'model-b' : 'model-a',
        })
      )
      expect(screen.getByText('Total: 5')).toBeVisible()
      if (tab === 'proportion') {
        await user.click(screen.getByRole('button', { name: 'model-a' }))
        expect(screen.getByText('Total: 0')).toBeVisible()
        await user.click(screen.getByRole('button', { name: 'model-a' }))
        expect(screen.getByText('Total: 5')).toBeVisible()
        await user.click(screen.getByRole('button', { name: 'model-b' }))
      } else {
        await user.click(screen.getByRole('button', { name: 'model-a' }))
      }
      expect(screen.getByText('Total: 12')).toBeVisible()
      await user.click(screen.getByRole('button', { name: 'By tokens' }))
      expect(screen.getByText('Total: 1,000')).toBeVisible()
      await user.click(screen.getByRole('button', { name: 'By requests' }))
      expect(screen.getByText('Total: 12')).toBeVisible()
    }
  )

  it('sums the merged Other series and clears selection when changing chart views', async () => {
    const user = userEvent.setup()
    const manyModels: QuotaDataItem[] = Array.from({ length: 22 }, (_, i) => ({
      created_at: 1_700_000_000,
      model_name: `model-${i}`,
      count: i + 1,
    }))
    render(<ModelCharts data={manyModels} />)
    expect(screen.getByText('Total: 253')).toBeVisible()
    await user.click(await screen.findByRole('button', { name: 'Other' }))
    expect(screen.getByText('Total: 3')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Call Count Ranking' }))
    expect(screen.getByText('Total: 253')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Call Trend' }))
    expect(screen.getByText('Total: 253')).toBeVisible()
  })
})

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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { toast } from 'sonner'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import {
  ModelPricingEditorPanel,
  type ModelRatioData,
} from '@/features/system-settings/models/model-pricing-sheet'
import { api } from '@/lib/api'
import { usePricingPreferencesStore } from '@/stores/pricing-preferences-store'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

const clients: QueryClient[] = []

beforeEach(() => {
  localStorage.clear()
  usePricingPreferencesStore.setState({ currency: 'USD' })
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG },
  })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: [], vendors: [] },
  })
  vi.spyOn(toast, 'success').mockReturnValue('copied')
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  clients.splice(0).forEach((client) => client.clear())
  usePricingPreferencesStore.setState({ currency: 'USD' })
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  localStorage.clear()
})

function renderCopyPaste() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], null)
  clients.push(client)

  function Harness() {
    const [editData, setEditData] = useState<ModelRatioData>({
      name: 'source-model',
      billingMode: 'per-token',
      ratio: '1',
      completionRatio: '',
    })
    const [copiedPricing, setCopiedPricing] = useState<ModelRatioData | null>(
      null
    )

    return (
      <>
        <button
          type='button'
          onClick={() =>
            setEditData({
              name: 'target-model',
              billingMode: 'per-token',
              ratio: '2',
              completionRatio: '',
            })
          }
        >
          Open target model
        </button>
        <ModelPricingEditorPanel
          editData={editData}
          copiedPricing={copiedPricing}
          onCopyPricing={setCopiedPricing}
        />
      </>
    )
  }

  render(
    <QueryClientProvider client={client}>
      <Harness />
    </QueryClientProvider>
  )
}

it('copies the unsaved editor draft and pastes it onto another model', async () => {
  const user = userEvent.setup()
  renderCopyPaste()

  expect(
    screen.queryByRole('button', { name: /^Paste .* pricing$/ })
  ).not.toBeInTheDocument()

  const input = screen.getByRole('textbox', { name: 'Input price' })
  await user.clear(input)
  await user.type(input, '3')

  await user.click(
    screen.getByRole('button', { name: 'Copy pricing configuration' })
  )
  const paste = await screen.findByRole('button', {
    name: 'Paste source-model pricing',
  })
  expect(paste).toBeVisible()

  await user.click(screen.getByRole('button', { name: 'Open target model' }))
  expect(screen.getByRole('textbox', { name: 'Model name' })).toHaveValue(
    'target-model'
  )
  expect(screen.getByRole('textbox', { name: 'Input price' })).toHaveValue('4')

  await user.click(paste)
  expect(screen.getByRole('textbox', { name: 'Input price' })).toHaveValue('3')
  expect(screen.getByRole('textbox', { name: 'Model name' })).toHaveValue(
    'target-model'
  )
  expect(toast.success).toHaveBeenCalledWith(
    'Pasted source-model pricing configuration'
  )
})

it('keeps the copied draft untouched by later edits to the target form', async () => {
  const user = userEvent.setup()
  renderCopyPaste()

  const input = screen.getByRole('textbox', { name: 'Input price' })
  await user.clear(input)
  await user.type(input, '3')
  await user.click(
    screen.getByRole('button', { name: 'Copy pricing configuration' })
  )

  await user.click(screen.getByRole('button', { name: 'Open target model' }))
  const paste = screen.getByRole('button', {
    name: 'Paste source-model pricing',
  })
  const targetInput = screen.getByRole('textbox', { name: 'Input price' })
  await user.clear(targetInput)
  await user.type(targetInput, '7')
  await user.click(paste)
  expect(screen.getByRole('textbox', { name: 'Input price' })).toHaveValue('3')
})

it('does not offer copy or paste when the panel has no clipboard wiring', () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], null)
  clients.push(client)

  render(
    <QueryClientProvider client={client}>
      <ModelPricingEditorPanel
        editData={{
          name: 'plain-model',
          billingMode: 'per-token',
          ratio: '1',
        }}
      />
    </QueryClientProvider>
  )

  expect(
    screen.queryByRole('button', { name: 'Copy pricing configuration' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: /^Paste .* pricing$/ })
  ).not.toBeInTheDocument()
})

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
import { afterEach, describe, expect, test, vi } from 'vitest'

import type { BillingUsageSchema } from '@/features/pricing/types'

import {
  buildCodexImportPreview,
  buildCodexPricingPrompt,
  createCodexPricingDocument,
  parseCodexPricingModels,
  serializeCodexPricingDocument,
} from '../model-pricing-codex'
import {
  CodexPricingExportDialog,
  CodexPricingImportDialog,
} from '../model-pricing-codex-dialog'
import type { ModelPricingSnapshot } from '../model-pricing-snapshots'

const snapshot = (
  name: string,
  values: Partial<ModelPricingSnapshot> = {}
): ModelPricingSnapshot => ({
  name,
  billingMode: 'per-token',
  hasConflict: false,
  ...values,
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('compact Codex pricing handoff', () => {
  test('exports unpriced, expression, legacy, and task models without internal fields', () => {
    const schemas = new Map<string, BillingUsageSchema>([
      [
        'video-model',
        {
          seconds: { type: 'number', unit: 'second' },
          quality: { enum: ['standard', 'pro'] },
        },
      ],
    ])
    const document = createCodexPricingDocument(
      [
        snapshot('unpriced-model'),
        snapshot('expression-model', {
          billingMode: 'tiered_expr',
          billingExpr: 'tier("base", p * 2 + c * 8)',
        }),
        snapshot('legacy-model', {
          ratio: '1.25',
          completionRatio: '3',
          cacheRatio: '0.1',
        }),
        snapshot('video-model'),
      ],
      schemas
    )

    expect(document.models).toEqual({
      'expression-model': 'tier("base", p * 2 + c * 8)',
      'legacy-model': { input: 2.5, output: 7.5, cache_read: 0.25 },
      'unpriced-model': null,
      'video-model': {
        $usage: { seconds: 'second', quality: ['standard', 'pro'] },
        $expr: null,
      },
    })
    const json = serializeCodexPricingDocument(document)
    expect(json).not.toContain('billing_setting')
    expect(json).not.toContain('hasConflict')
    expect(json).not.toContain('effective')
    expect(buildCodexPricingPrompt(document)).toContain(
      'Every leaf branch must be wrapped in tier'
    )
    expect(buildCodexPricingPrompt(document)).toContain('unpriced-model')
  })

  test('accepts a full document, a bare model map, and a fenced JSON block', () => {
    expect(
      parseCodexPricingModels(
        '{"$rules":[],"models":{"model-a":"tier(\\"base\\", p * 1)"}}'
      )
    ).toEqual({ 'model-a': 'tier("base", p * 1)' })
    expect(parseCodexPricingModels('{"model-a":null}')).toEqual({
      'model-a': null,
    })
    expect(
      parseCodexPricingModels('```json\n{"models":{"model-a":null}}\n```')
    ).toEqual({ 'model-a': null })
  })

  test('validates expressions, task usage fields, nulls, and extra models', () => {
    const current = new Map<string, ModelPricingSnapshot>([
      ['known', snapshot('known')],
      [
        'task',
        snapshot('task', {
          billingMode: 'tiered_expr',
          billingExpr: 'tier("base", u("seconds") * 0.1)',
        }),
      ],
      ['task-unpriced', snapshot('task-unpriced')],
    ])
    const schemas = new Map<string, BillingUsageSchema>([
      ['task', { seconds: { type: 'number', unit: 'second' } }],
      ['task-unpriced', { seconds: { type: 'number', unit: 'second' } }],
    ])
    const preview = buildCodexImportPreview(
      JSON.stringify({
        models: {
          known: 'tier("base", p * 1 + c * 2)',
          task: {
            $usage: { seconds: 'second' },
            $expr: 'tier("base", u("minutes") * 1)',
          },
          'task-unpriced': {
            $usage: { seconds: 'second' },
            $expr: 'tier("base", u("seconds") * 0.1)',
          },
          unresolved: null,
          extra: 'tier("base", fixed(0.01))',
        },
      }),
      current,
      schemas
    )

    expect(preview.find((item) => item.name === 'known')).toMatchObject({
      status: 'new',
      extra: false,
      draft: { billingMode: 'tiered_expr' },
    })
    expect(preview.find((item) => item.name === 'task')).toMatchObject({
      status: 'error',
      error: 'Unknown usage field: minutes',
    })
    expect(preview.find((item) => item.name === 'task-unpriced')).toMatchObject(
      {
        status: 'new',
        extra: false,
      }
    )
    expect(preview.find((item) => item.name === 'unresolved')).toMatchObject({
      status: 'unresolved',
    })
    expect(preview.find((item) => item.name === 'extra')).toMatchObject({
      status: 'new',
      extra: true,
    })
  })

  test('merges legacy fields without dropping implicit output pricing', () => {
    const current = new Map<string, ModelPricingSnapshot>([
      [
        'legacy',
        snapshot('legacy', {
          ratio: '1',
          completionRatio: '2',
          cacheRatio: '0.5',
        }),
      ],
    ])
    const [preview] = buildCodexImportPreview(
      '{"models":{"legacy":{"output":null}}}',
      current,
      new Map()
    )

    expect(preview.status).toBe('update')
    expect(preview.draft).toMatchObject({
      billingMode: 'per-token',
      ratio: '1',
      completionRatio: '',
      cacheRatio: '0.5',
    })
    expect(preview.draft?.billingExpr).toBeUndefined()
  })

  test('uses an expression when compact prices cannot be represented by ratios', () => {
    const [preview] = buildCodexImportPreview(
      '{"models":{"image-model":{"input":2,"image_output":8}}}',
      new Map(),
      new Map()
    )

    expect(preview.draft).toMatchObject({
      billingMode: 'tiered_expr',
      billingExpr: 'tier("base", p * 2 + img_o * 8)',
    })
  })
})

describe('Codex pricing export dialog', () => {
  test('loads a JSON file and applies valid pricing to the draft only', async () => {
    const user = userEvent.setup()
    const onApply = vi.fn()
    render(
      <CodexPricingImportDialog
        open
        onOpenChange={() => undefined}
        currentSnapshots={new Map([['model-a', snapshot('model-a')]])}
        usageSchemas={new Map()}
        onApply={onApply}
      />
    )
    const input = document.querySelector<HTMLInputElement>('input[type="file"]')
    expect(input).not.toBeNull()
    const file = new File(
      [
        JSON.stringify({
          models: {
            'model-a': 'tier("base", p * 1 + c * 2)',
            'extra-model': 'tier("base", p * 3)',
          },
        }),
      ],
      'pricing.json',
      { type: 'application/json' }
    )

    await user.upload(input as HTMLInputElement, file)
    expect(await screen.findAllByText('New expression')).toHaveLength(2)
    await user.click(screen.getByRole('button', { name: 'Apply to draft' }))

    expect(onApply).toHaveBeenCalledWith([
      expect.objectContaining({
        name: 'model-a',
        billingMode: 'tiered_expr',
        billingExpr: 'tier("base", p * 1 + c * 2)',
      }),
    ])
  })
  test('copies the shared prompt and downloads the compact JSON', async () => {
    const user = userEvent.setup()
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })
    let downloaded: Blob | undefined
    let downloadName = ''
    vi.stubGlobal(
      'URL',
      Object.assign(class extends URL {}, {
        createObjectURL: vi.fn((blob: Blob) => {
          downloaded = blob
          return 'blob:codex-pricing'
        }),
        revokeObjectURL: vi.fn(),
      })
    )
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(function (this: HTMLAnchorElement) {
        downloadName = this.download
      })

    render(
      <CodexPricingExportDialog
        open
        onOpenChange={() => undefined}
        snapshots={[snapshot('model-a')]}
        usageSchemas={new Map()}
      />
    )

    await user.click(screen.getByRole('button', { name: 'Copy Codex prompt' }))
    expect(writeText).toHaveBeenCalledWith(
      expect.stringContaining('Every leaf branch must be wrapped in tier')
    )
    await user.click(screen.getByRole('button', { name: 'Download JSON' }))
    expect(click).toHaveBeenCalledOnce()
    expect(downloaded).toBeInstanceOf(Blob)
    expect(downloaded?.type).toBe('application/json;charset=utf-8')
    const date = new Date()
    const day = [
      date.getFullYear(),
      String(date.getMonth() + 1).padStart(2, '0'),
      String(date.getDate()).padStart(2, '0'),
    ].join('-')
    expect(downloadName).toBe(`model-pricing-codex-${day}.json`)
    expect(await downloaded?.text()).toContain('"model-a": null')
  })
})

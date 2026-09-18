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
import {
  combineBillingExpr,
  splitBillingExprAndRequestRules,
} from '@/features/pricing/lib/billing-expr'
import { compileBillingExpression } from '@/features/pricing/lib/billing-expression/parser'
import { visitExpression } from '@/features/pricing/lib/billing-expression/types'
import type { BillingUsageSchema } from '@/features/pricing/types'

import { LLM_PROMPT_TEMPLATE } from './billing-expression-prompt'
import type { ModelRatioData } from './model-pricing-core'
import type { ModelPricingSnapshot } from './model-pricing-snapshots'
import { formatPricingNumber } from './pricing-format'

export const CODEX_PRICING_RULES = [
  'Research official provider pricing in USD.',
  'Prefer new-api billing expressions and keep model names unchanged.',
  'Token coefficients are USD per 1M tokens. Use tier(...), and use len for context tiers.',
  'For task models use u("field"); non-token fields are USD per unit and token fields are USD per 1M.',
  'Keep null when reliable pricing cannot be found. Return JSON only.',
] as const

const LEGACY_FIELDS = [
  'request',
  'input',
  'output',
  'cache_read',
  'cache_write',
  'image_input',
  'image_output',
  'audio_input',
  'audio_output',
] as const

type LegacyField = (typeof LEGACY_FIELDS)[number]
export type CompactLegacyPricing = Partial<Record<LegacyField, number | null>>
export type CompactUsageSchema = Record<string, string | string[]>
export type CompactTaskPricing = {
  $usage: CompactUsageSchema
  $expr: string | null
}
export type CodexPricingValue =
  | null
  | string
  | CompactLegacyPricing
  | CompactTaskPricing

export type CodexPricingDocument = {
  $rules: readonly string[]
  models: Record<string, CodexPricingValue>
}

export type CodexImportStatus =
  | 'unchanged'
  | 'new'
  | 'update'
  | 'unresolved'
  | 'error'

export type CodexImportPreview = {
  name: string
  status: CodexImportStatus
  extra: boolean
  current: CodexPricingValue
  next: CodexPricingValue
  draft?: ModelRatioData
  error?: string
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === 'object' && !Array.isArray(value)

const toCompactNumber = (value: number): number =>
  Number(formatPricingNumber(value))

function compactUsageSchema(schema: BillingUsageSchema): CompactUsageSchema {
  return Object.fromEntries(
    Object.entries(schema).map(([field, definition]) => {
      if (definition.enum?.length) return [field, [...definition.enum]]
      if (definition.type === 'boolean') return [field, 'boolean']
      return [field, definition.unit ?? 'number']
    })
  )
}

function snapshotToLegacy(
  snapshot: ModelPricingSnapshot
): CompactLegacyPricing | null {
  if (snapshot.billingMode === 'per-request' && snapshot.price) {
    return { request: Number(snapshot.price) }
  }

  if (!snapshot.ratio) return null
  const input = Number(snapshot.ratio) * 2
  if (!Number.isFinite(input)) return null
  const legacy: CompactLegacyPricing = { input: toCompactNumber(input) }
  const addRelative = (
    field: keyof ModelPricingSnapshot,
    output: LegacyField,
    base = input
  ) => {
    const raw = snapshot[field]
    if (typeof raw !== 'string' || raw === '') return
    const value = Number(raw) * base
    if (Number.isFinite(value)) legacy[output] = toCompactNumber(value)
  }
  addRelative('completionRatio', 'output')
  addRelative('cacheRatio', 'cache_read')
  addRelative('createCacheRatio', 'cache_write')
  addRelative('imageRatio', 'image_input')
  addRelative('audioRatio', 'audio_input')
  if (legacy.audio_input !== undefined && legacy.audio_input !== null) {
    addRelative('audioCompletionRatio', 'audio_output', legacy.audio_input)
  }
  return legacy
}

export function snapshotToCodexPricing(
  snapshot: ModelPricingSnapshot,
  usageSchema?: BillingUsageSchema
): CodexPricingValue {
  const fullExpression = snapshot.billingExpr
    ? combineBillingExpr(snapshot.billingExpr, snapshot.requestRuleExpr ?? '')
    : null
  if (usageSchema && Object.keys(usageSchema).length > 0) {
    return {
      $usage: compactUsageSchema(usageSchema),
      $expr: fullExpression,
    }
  }
  if (snapshot.billingMode === 'tiered_expr' && fullExpression) {
    return fullExpression
  }
  return snapshotToLegacy(snapshot)
}

export function createCodexPricingDocument(
  snapshots: ModelPricingSnapshot[],
  usageSchemas: ReadonlyMap<string, BillingUsageSchema>
): CodexPricingDocument {
  const models = Object.fromEntries(
    [...snapshots]
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((snapshot) => [
        snapshot.name,
        snapshotToCodexPricing(snapshot, usageSchemas.get(snapshot.name)),
      ])
  )
  return { $rules: CODEX_PRICING_RULES, models }
}

export function serializeCodexPricingDocument(
  document: CodexPricingDocument
): string {
  return `${JSON.stringify(document, null, 2)}\n`
}

export function buildCodexPricingPrompt(
  document: CodexPricingDocument
): string {
  return `${LLM_PROMPT_TEMPLATE}

## Batch task

Research the selected models and edit the JSON in place. Preserve existing non-null expressions unless they are incomplete or incorrect. Prefer filling missing or incomplete pricing with billing expressions. Return only the completed JSON.

\`\`\`json
${JSON.stringify(document, null, 2)}
\`\`\``
}

function extractJsonText(value: string): string {
  const trimmed = value.trim()
  const match = trimmed.match(/^```(?:json)?\s*([\s\S]*?)\s*```$/i)
  return match ? match[1] : trimmed
}

export function parseCodexPricingModels(
  value: string
): Record<string, unknown> {
  const parsed: unknown = JSON.parse(extractJsonText(value))
  if (!isRecord(parsed)) throw new Error('Pricing JSON must be an object')
  const source = 'models' in parsed ? parsed.models : parsed
  if (!isRecord(source)) throw new Error('models must be an object')
  return Object.fromEntries(
    Object.entries(source).filter(([name]) => !name.startsWith('$'))
  )
}

function validateUsageSchema(value: unknown): CompactUsageSchema {
  if (!isRecord(value)) throw new Error('$usage must be an object')
  const schema: CompactUsageSchema = {}
  for (const [field, definition] of Object.entries(value)) {
    if (
      typeof definition === 'string' ||
      (Array.isArray(definition) &&
        definition.length > 0 &&
        definition.every((item) => typeof item === 'string'))
    ) {
      schema[field] = definition as string | string[]
      continue
    }
    throw new Error(`Invalid usage field: ${field}`)
  }
  return schema
}

function validateExpression(
  expression: string,
  usageFields?: ReadonlySet<string>
): void {
  if (!expression.trim()) throw new Error('Billing expression is empty')
  const compiled = compileBillingExpression(expression)
  if (compiled.status !== 'ready') throw new Error(compiled.diagnostic.detail)

  const referencedUsage = new Set<string>()
  visitExpression(compiled.ast, (node) => {
    if (node.kind !== 'call' || node.name !== 'u') return
    const field = node.args[0]
    if (
      field?.kind !== 'literal' ||
      typeof field.value !== 'string' ||
      !field.value
    ) {
      throw new Error('u() requires a literal usage field name')
    }
    referencedUsage.add(field.value)
  })

  if (!usageFields) {
    if (referencedUsage.size > 0) {
      throw new Error('Task usage schema is required for u() expressions')
    }
    return
  }
  if (compiled.variables.size > 0 || compiled.functions.has('fixed')) {
    throw new Error(
      'Task expressions must use usage fields instead of token variables or fixed()'
    )
  }
  for (const field of referencedUsage) {
    if (!usageFields.has(field)) {
      throw new Error(`Unknown usage field: ${field}`)
    }
  }
}

function validateLegacyPricing(
  value: Record<string, unknown>
): CompactLegacyPricing {
  const result: CompactLegacyPricing = {}
  for (const [field, price] of Object.entries(value)) {
    if (!LEGACY_FIELDS.includes(field as LegacyField)) {
      throw new Error(`Unknown pricing field: ${field}`)
    }
    if (price === null) {
      result[field as LegacyField] = null
      continue
    }
    if (typeof price !== 'number' || !Number.isFinite(price) || price < 0) {
      throw new Error(`Invalid price: ${field}`)
    }
    result[field as LegacyField] = price
  }
  if (Object.keys(result).length === 0) {
    throw new Error('Pricing object is empty')
  }
  return result
}

function parsePricingValue(value: unknown): CodexPricingValue {
  if (value === null) return null
  if (typeof value === 'string') return value
  if (!isRecord(value)) {
    throw new Error('Model pricing must be null, a string, or an object')
  }
  if ('$usage' in value || '$expr' in value) {
    const unknown = Object.keys(value).filter(
      (key) => key !== '$usage' && key !== '$expr'
    )
    if (unknown.length) {
      throw new Error(`Unknown task pricing field: ${unknown[0]}`)
    }
    const usage = validateUsageSchema(value.$usage)
    if (value.$expr !== null && typeof value.$expr !== 'string') {
      throw new Error('$expr must be a string or null')
    }
    return { $usage: usage, $expr: value.$expr as string | null }
  }
  return validateLegacyPricing(value)
}

const isTaskPricing = (value: CodexPricingValue): value is CompactTaskPricing =>
  isRecord(value) && '$usage' in value

const isLegacyPricing = (
  value: CodexPricingValue
): value is CompactLegacyPricing => isRecord(value) && !('$usage' in value)

function hasConfiguredPricing(value: CodexPricingValue): boolean {
  if (value === null) return false
  if (typeof value === 'string') return value.trim() !== ''
  if (isTaskPricing(value)) return Boolean(value.$expr?.trim())
  return Object.values(value).some((price) => price !== null)
}

function canonical(value: CodexPricingValue): string {
  if (!isRecord(value)) return JSON.stringify(value)
  return JSON.stringify(
    Object.fromEntries(
      Object.entries(value)
        .map(([key, item]) => [
          key,
          isRecord(item)
            ? Object.fromEntries(
                Object.entries(item).sort(([a], [b]) => a.localeCompare(b))
              )
            : item,
        ])
        .sort(([a], [b]) => a.localeCompare(b))
    )
  )
}

function expressionDraft(name: string, expression: string): ModelRatioData {
  const parts = splitBillingExprAndRequestRules(expression)
  return {
    name,
    billingMode: 'tiered_expr',
    billingExpr: parts.billingExpr,
    requestRuleExpr: parts.requestRuleExpr,
    price: '',
    ratio: '',
    completionRatio: '',
    cacheRatio: '',
    createCacheRatio: '',
    imageRatio: '',
    audioRatio: '',
    audioCompletionRatio: '',
  }
}

function clearDraft(name: string): ModelRatioData {
  return {
    name,
    billingMode: 'per-token',
    price: '',
    ratio: '',
    completionRatio: '',
    cacheRatio: '',
    createCacheRatio: '',
    imageRatio: '',
    audioRatio: '',
    audioCompletionRatio: '',
  }
}

function legacyToExpression(value: CompactLegacyPricing): string | null {
  const active = Object.fromEntries(
    Object.entries(value).filter(([, price]) => price !== null)
  ) as Partial<Record<LegacyField, number>>
  if (Object.keys(active).length === 0) return null
  const tokenFields = LEGACY_FIELDS.filter(
    (field): field is Exclude<LegacyField, 'request'> =>
      field !== 'request' && active[field] !== undefined
  )
  if (active.request !== undefined) {
    if (tokenFields.length > 0) {
      throw new Error('Request pricing cannot be combined with token pricing')
    }
    return `tier("base", fixed(${active.request}))`
  }
  const variables: Record<Exclude<LegacyField, 'request'>, string> = {
    input: 'p',
    output: 'c',
    cache_read: 'cr',
    cache_write: 'cc',
    image_input: 'img',
    image_output: 'img_o',
    audio_input: 'ai',
    audio_output: 'ao',
  }
  const terms = tokenFields.map(
    (field) => `${variables[field]} * ${active[field]}`
  )
  return terms.length ? `tier("base", ${terms.join(' + ')})` : null
}

function legacyToDraft(
  name: string,
  value: CompactLegacyPricing
): ModelRatioData {
  const active = Object.fromEntries(
    Object.entries(value).filter(([, price]) => price !== null)
  ) as Partial<Record<LegacyField, number>>
  if (Object.keys(active).length === 0) return clearDraft(name)

  const tokenFields = LEGACY_FIELDS.filter(
    (field): field is Exclude<LegacyField, 'request'> =>
      field !== 'request' && active[field] !== undefined
  )
  if (active.request !== undefined) {
    if (tokenFields.length > 0) {
      throw new Error('Request pricing cannot be combined with token pricing')
    }
    return {
      name,
      billingMode: 'per-request',
      price: formatPricingNumber(active.request),
      ratio: '',
      completionRatio: '',
      cacheRatio: '',
      createCacheRatio: '',
      imageRatio: '',
      audioRatio: '',
      audioCompletionRatio: '',
    }
  }

  const input = active.input
  const dependentPrices = tokenFields
    .filter((field) => field !== 'input')
    .map((field) => active[field] ?? 0)
  const needsExpression =
    input === undefined ||
    active.image_output !== undefined ||
    (input === 0 && dependentPrices.some((price) => price > 0))
  if (needsExpression) {
    const expression = legacyToExpression(active)
    return expression ? expressionDraft(name, expression) : clearDraft(name)
  }

  const relative = (price?: number, base = input): string =>
    price === undefined ? '' : formatPricingNumber(price / base)
  const audioInput = active.audio_input ?? input
  let audioRatio = ''
  if (active.audio_input !== undefined) {
    audioRatio = relative(active.audio_input)
  } else if (active.audio_output !== undefined) {
    audioRatio = '1'
  }

  return {
    name,
    billingMode: 'per-token',
    price: '',
    ratio: formatPricingNumber(input / 2),
    completionRatio: relative(active.output),
    cacheRatio: relative(active.cache_read),
    createCacheRatio: relative(active.cache_write),
    imageRatio: relative(active.image_input),
    audioRatio,
    audioCompletionRatio: relative(active.audio_output, audioInput),
  }
}

export function buildCodexImportPreview(
  text: string,
  currentSnapshots: ReadonlyMap<string, ModelPricingSnapshot>,
  usageSchemas: ReadonlyMap<string, BillingUsageSchema>
): CodexImportPreview[] {
  const models = parseCodexPricingModels(text)
  return Object.entries(models).map(([name, rawValue]) => {
    const currentSnapshot = currentSnapshots.get(name)
    const knownUsage = usageSchemas.get(name)
    const current = currentSnapshot
      ? snapshotToCodexPricing(currentSnapshot, knownUsage)
      : null
    const extra = !currentSnapshot
    try {
      const next = parsePricingValue(rawValue)
      if (next === null) {
        return { name, status: 'unresolved', extra, current, next }
      }
      if (canonical(current) === canonical(next)) {
        return { name, status: 'unchanged', extra, current, next }
      }

      let expression: string | null = null
      let draft: ModelRatioData
      if (typeof next === 'string') {
        expression = next
        const fields = knownUsage ? new Set(Object.keys(knownUsage)) : undefined
        validateExpression(expression, fields)
        draft = expressionDraft(name, expression)
      } else if (isTaskPricing(next)) {
        expression = next.$expr
        if (expression === null) {
          return { name, status: 'unresolved', extra, current, next }
        }
        const fields = new Set(Object.keys(knownUsage ?? next.$usage))
        validateExpression(expression, fields)
        draft = expressionDraft(name, expression)
      } else if (isLegacyPricing(next)) {
        if (typeof current === 'string' || isTaskPricing(current)) {
          throw new Error(
            'Use a billing expression string to update this model'
          )
        }
        const merged: CompactLegacyPricing = {
          ...(isLegacyPricing(current) ? current : {}),
          ...next,
        }
        draft = legacyToDraft(name, merged)
        expression =
          draft.billingMode === 'tiered_expr'
            ? combineBillingExpr(
                draft.billingExpr ?? '',
                draft.requestRuleExpr ?? ''
              )
            : null
      } else {
        throw new Error('Unsupported pricing value')
      }

      return {
        name,
        status: hasConfiguredPricing(current) ? 'update' : 'new',
        extra,
        current,
        next: expression ?? next,
        draft,
      }
    } catch (error) {
      return {
        name,
        status: 'error',
        extra,
        current,
        next: rawValue as CodexPricingValue,
        error: error instanceof Error ? error.message : String(error),
      }
    }
  })
}

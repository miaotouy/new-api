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
import { Download, Upload } from 'lucide-react'
import { useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { BillingUsageSchema } from '@/features/pricing/types'

import type { ModelRatioData } from './model-pricing-core'
import {
  buildImportPreview,
  buildPricingPrompt,
  createPricingDocument,
  serializePricingDocument,
  type ImportPreview,
  type PricingValue,
} from './model-pricing-handoff'
import type { ModelPricingSnapshot } from './model-pricing-snapshots'

const MAX_JSON_FILE_SIZE = 1024 * 1024

function readJsonFile(file: File): Promise<string> {
  if (typeof file.text === 'function') return file.text()
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.addEventListener('load', () => resolve(String(reader.result)), {
      once: true,
    })
    reader.addEventListener('error', () => reject(reader.error), { once: true })
    reader.readAsText(file)
  })
}

function formatPricingValue(value: PricingValue): string {
  if (value === null) return 'null'
  if (typeof value === 'string') return value
  return JSON.stringify(value)
}

function downloadJson(content: string): void {
  const blob = new Blob([content], { type: 'application/json;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  const date = new Date()
  const day = [
    date.getFullYear(),
    String(date.getMonth() + 1).padStart(2, '0'),
    String(date.getDate()).padStart(2, '0'),
  ].join('-')
  link.href = url
  link.download = `model-pricing-handoff-${day}.json`
  document.body.append(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

type PricingExportDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  snapshots: ModelPricingSnapshot[]
  usageSchemas: ReadonlyMap<string, BillingUsageSchema>
}

export function PricingExportDialog(props: PricingExportDialogProps) {
  const { t } = useTranslation()
  const document = useMemo(
    () => createPricingDocument(props.snapshots, props.usageSchemas),
    [props.snapshots, props.usageSchemas]
  )
  const json = useMemo(() => serializePricingDocument(document), [document])
  const prompt = useMemo(() => buildPricingPrompt(document), [document])

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Export model pricing')}
      description={t(
        'Use this compact JSON with an AI agent to research and complete model pricing.'
      )}
      contentClassName='sm:max-w-4xl'
      contentHeight='min(66vh, 620px)'
      footer={
        <>
          <CopyButton
            value={json}
            variant='outline'
            size='default'
            tooltip={t('Copy JSON')}
            successTooltip={t('Pricing JSON copied to clipboard')}
            aria-label={t('Copy JSON')}
          >
            {t('Copy JSON')}
          </CopyButton>
          <CopyButton
            value={prompt}
            variant='outline'
            size='default'
            tooltip={t('Copy agent prompt')}
            successTooltip={t('Agent prompt copied to clipboard')}
            aria-label={t('Copy agent prompt')}
          >
            {t('Copy agent prompt')}
          </CopyButton>
          <Button onClick={() => downloadJson(json)}>
            <Download data-icon='inline-start' />
            {t('Download JSON')}
          </Button>
        </>
      }
    >
      <JsonCodeEditor
        value={json}
        onChange={() => undefined}
        readOnly
        ariaLabel={t('Model pricing JSON')}
        heightClassName='h-full min-h-96 max-h-none'
      />
    </Dialog>
  )
}

type PricingImportDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentSnapshots: ReadonlyMap<string, ModelPricingSnapshot>
  usageSchemas: ReadonlyMap<string, BillingUsageSchema>
  onApply: (drafts: ModelRatioData[]) => void
}

export function PricingImportDialog(props: PricingImportDialogProps) {
  const { t } = useTranslation()
  const inputRef = useRef<HTMLInputElement>(null)
  const [value, setValue] = useState('')
  const [includedExtra, setIncludedExtra] = useState<Set<string>>(new Set())
  let preview: ImportPreview[] = []
  let parseError = ''
  if (value.trim()) {
    try {
      preview = buildImportPreview(
        value,
        props.currentSnapshots,
        props.usageSchemas
      )
    } catch (error) {
      parseError = error instanceof Error ? error.message : String(error)
    }
  }

  const updateValue = (next: string) => {
    setValue(next)
    setIncludedExtra(new Set())
  }
  const hasErrors = Boolean(parseError)
  const skippedCount = preview.filter(
    (item) => item.status === 'error' || item.status === 'unresolved'
  ).length
  const drafts = preview
    .filter(
      (item) =>
        item.draft &&
        (item.status === 'new' || item.status === 'update') &&
        (!item.extra || includedExtra.has(item.name))
    )
    .map((item) => item.draft as ModelRatioData)

  const statusLabel = (status: ImportPreview['status']) => {
    if (status === 'unchanged') return t('Unchanged')
    if (status === 'new') return t('New expression')
    if (status === 'update') return t('Updated pricing')
    if (status === 'unresolved') return t('Not found')
    return t('Invalid')
  }

  const handleFile = async (file?: File) => {
    if (!file) return
    if (file.size > MAX_JSON_FILE_SIZE) {
      toast.error(t('JSON file must be 1 MB or smaller'))
      return
    }
    try {
      updateValue(await readJsonFile(file))
    } catch {
      toast.error(t('Failed to read JSON file'))
    }
  }

  const handleApply = () => {
    if (hasErrors || drafts.length === 0) return
    props.onApply(drafts)
    toast.success(
      t('Imported {{count}} model pricing drafts', { count: drafts.length })
    )
    props.onOpenChange(false)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Import model pricing')}
      description={t(
        'Paste the JSON returned by an agent or upload a .json file.'
      )}
      contentClassName='sm:max-w-5xl'
      contentHeight='min(72vh, 720px)'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            onClick={handleApply}
            disabled={hasErrors || drafts.length === 0}
          >
            {t('Apply to draft')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        <div className='flex justify-end'>
          <input
            ref={inputRef}
            type='file'
            accept='application/json,.json'
            className='sr-only'
            onChange={(event) => {
              void handleFile(event.target.files?.[0])
              event.target.value = ''
            }}
          />
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => inputRef.current?.click()}
          >
            <Upload data-icon='inline-start' />
            {t('Upload JSON')}
          </Button>
        </div>
        <JsonCodeEditor
          value={value}
          onChange={updateValue}
          ariaLabel={t('Model pricing JSON')}
          placeholder='{"models":{"model-name":"tier(\"base\", p * 1 + c * 2)"}}'
          heightClassName='h-72 min-h-72 max-h-72'
        />
        {parseError && (
          <Alert variant='destructive'>
            <AlertDescription>
              {t('Invalid model pricing JSON')}
            </AlertDescription>
          </Alert>
        )}
        {!parseError && preview.length > 0 && (
          <div className='overflow-hidden rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Model')}</TableHead>
                  <TableHead>{t('Status')}</TableHead>
                  <TableHead>{t('Pricing result')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {preview.map((item) => (
                  <TableRow key={item.name}>
                    <TableCell className='max-w-64 align-top font-mono break-all whitespace-normal'>
                      <div className='flex items-start gap-2'>
                        {item.extra && (
                          <Checkbox
                            checked={includedExtra.has(item.name)}
                            onCheckedChange={(checked) => {
                              setIncludedExtra((current) => {
                                const next = new Set(current)
                                if (checked) next.add(item.name)
                                else next.delete(item.name)
                                return next
                              })
                            }}
                            aria-label={t('Include extra model {{name}}', {
                              name: item.name,
                            })}
                          />
                        )}
                        <span>{item.name}</span>
                      </div>
                      {item.extra && (
                        <Badge variant='outline' className='mt-2'>
                          {t('Extra model')}
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className='align-top'>
                      <Badge
                        variant={
                          item.status === 'error' ? 'destructive' : 'outline'
                        }
                      >
                        {statusLabel(item.status)}
                      </Badge>
                    </TableCell>
                    <TableCell className='max-w-xl align-top whitespace-normal'>
                      <pre className='text-xs break-words whitespace-pre-wrap'>
                        {item.error
                          ? t('Invalid pricing value')
                          : formatPricingValue(item.next)}
                      </pre>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        {skippedCount > 0 && !parseError && drafts.length > 0 && (
          <Alert>
            <AlertDescription>
              {t(
                'Some entries will be skipped because no valid pricing was found.',
                {
                  count: skippedCount,
                }
              )}
            </AlertDescription>
          </Alert>
        )}
        {!hasErrors && preview.length > 0 && drafts.length === 0 && (
          <Alert>
            <AlertDescription>
              {t('No pricing changes to apply')}
            </AlertDescription>
          </Alert>
        )}
      </div>
    </Dialog>
  )
}

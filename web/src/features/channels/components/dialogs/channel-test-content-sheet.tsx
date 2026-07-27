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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowDown, ArrowUp, Minus, Plus } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import { getChannelTestConfig, updateChannelTestConfig } from '../../api'
import { channelsQueryKeys } from '../../lib'
import type {
  ChannelTestConfigResponse,
  ChannelTestContentOverride,
  ChannelTestContentSource,
  ChannelTestEndpoint,
} from '../../types'

const endpoints: ChannelTestEndpoint[] = [
  'openai',
  'openai-response',
  'openai-response-compact',
  'anthropic',
  'gemini',
  'jina-rerank',
  'image-generation',
  'embeddings',
]

const MAX_CONTENT_LENGTH = 4096
const MAX_DOCUMENTS = 8
const MAX_DOCUMENTS_TOTAL_LENGTH = 16384

type ChannelTestOverrides = Partial<
  Record<ChannelTestEndpoint, ChannelTestContentOverride>
>

type ChannelTestContentSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channelId: number
  endpointType: string
  sessionOverrides: ChannelTestOverrides
  onApply: (overrides: ChannelTestOverrides) => void
}

function isEndpoint(value: string): value is ChannelTestEndpoint {
  return endpoints.includes(value as ChannelTestEndpoint)
}

function textValue(override: ChannelTestContentOverride | undefined) {
  return override?.content ?? override?.input ?? override?.prompt ?? ''
}

function characterCount(value: string) {
  return [...value].length
}

function validateOverride(
  endpoint: ChannelTestEndpoint,
  override: ChannelTestContentOverride | undefined
): string | null {
  if (!override || override.mode !== 'custom') return null

  if (endpoint === 'jina-rerank') {
    if (!override.query?.trim()) return 'Test content cannot be empty'
    const documents = override.documents ?? []
    if (documents.length < 1 || documents.length > MAX_DOCUMENTS) {
      return 'Add between 1 and 8 documents'
    }
    if (documents.some((document) => !document.trim())) {
      return 'Each document must contain text'
    }
    if (
      documents.some(
        (document) => characterCount(document) > MAX_CONTENT_LENGTH
      )
    ) {
      return 'Each document must be at most 4096 characters'
    }
    if (characterCount(override.query) > MAX_CONTENT_LENGTH) {
      return 'Query must be at most 4096 characters'
    }
    if (
      documents.reduce(
        (total, document) => total + characterCount(document),
        0
      ) > MAX_DOCUMENTS_TOTAL_LENGTH
    ) {
      return 'Documents must be at most 16384 characters total'
    }
    return null
  }

  const value = textValue(override)
  if (!value.trim()) return 'Test content cannot be empty'
  if (characterCount(value) > MAX_CONTENT_LENGTH) {
    return 'Test content must be at most 4096 characters'
  }
  return null
}

function createDocumentId(counter: { value: number }) {
  counter.value += 1
  return `document-${counter.value}`
}

export function ChannelTestContentSheet({
  open,
  onOpenChange,
  channelId,
  endpointType,
  sessionOverrides,
  onApply,
}: ChannelTestContentSheetProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const canWrite = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.WRITE
  )
  const [selectedEndpoint, setSelectedEndpoint] = useState<ChannelTestEndpoint>(
    isEndpoint(endpointType) ? endpointType : 'openai'
  )
  const [draftOverrides, setDraftOverrides] = useState(sessionOverrides)
  const [documentIds, setDocumentIds] = useState<string[]>([])
  const documentIdCounter = useRef({ value: 0 })
  const sessionOverridesRef = useRef(sessionOverrides)

  const configQuery = useQuery<ChannelTestConfigResponse['data']>({
    queryKey: channelsQueryKeys.testConfig(channelId),
    queryFn: async () => {
      const response = await getChannelTestConfig(channelId)
      if (!response.success || !response.data) {
        throw new Error(response.message || t('Failed to load test content'))
      }
      return response.data
    },
    enabled: open,
  })

  const saveMutation = useMutation({
    mutationFn: async (config: {
      version: number
      overrides: ChannelTestOverrides
      endpoint: ChannelTestEndpoint
      sessionOverrides: ChannelTestOverrides
    }) => {
      const response = await updateChannelTestConfig(channelId, {
        version: config.version,
        overrides: config.overrides,
      })
      if (!response.success) {
        throw new Error(response.message || t('Failed to save test content'))
      }
      return config
    },
    onSuccess: (config) => {
      queryClient.setQueryData(
        channelsQueryKeys.testConfig(channelId),
        (current) =>
          current ? { ...current, overrides: config.overrides } : current
      )
      setDraftOverrides(config.sessionOverrides)
      onApply(config.sessionOverrides)
      toast.success(t('Channel test content saved'))
    },
    onError: (error: unknown) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to save test content')
      )
    },
  })

  const savedOverrides: ChannelTestOverrides = configQuery.data?.overrides ?? {}
  const builtinOverrides: Record<
    ChannelTestEndpoint,
    ChannelTestContentOverride
  > =
    configQuery.data?.builtin_overrides ??
    ({} as Record<ChannelTestEndpoint, ChannelTestContentOverride>)
  const currentOverride = draftOverrides[selectedEndpoint]
  const visibleOverride =
    currentOverride?.mode === 'builtin'
      ? builtinOverrides[selectedEndpoint]
      : (currentOverride ??
        savedOverrides[selectedEndpoint] ??
        builtinOverrides[selectedEndpoint])
  const source: ChannelTestContentSource = currentOverride?.mode ?? 'inherit'
  const validationMessage = validateOverride(selectedEndpoint, currentOverride)
  const isAutoEndpoint = !isEndpoint(endpointType)
  const loading = configQuery.isLoading
  const saving = saveMutation.isPending

  useEffect(() => {
    sessionOverridesRef.current = sessionOverrides
  }, [sessionOverrides])

  useEffect(() => {
    if (!open) return
    setSelectedEndpoint(isEndpoint(endpointType) ? endpointType : 'openai')
    setDraftOverrides(sessionOverridesRef.current)
  }, [channelId, endpointType, open])

  const rerankDocuments = currentOverride?.documents ?? ['']

  useEffect(() => {
    if (selectedEndpoint !== 'jina-rerank') return
    setDocumentIds((previous) => {
      if (previous.length === rerankDocuments.length) return previous
      if (previous.length > rerankDocuments.length) {
        return previous.slice(0, rerankDocuments.length)
      }
      return [
        ...previous,
        ...Array.from(
          { length: rerankDocuments.length - previous.length },
          () => createDocumentId(documentIdCounter.current)
        ),
      ]
    })
  }, [rerankDocuments.length, selectedEndpoint])

  const updateDraft = (override: ChannelTestContentOverride | undefined) => {
    setDraftOverrides((previous) => ({
      ...previous,
      ...(override
        ? { [selectedEndpoint]: override }
        : Object.fromEntries(
            Object.entries(previous).filter(([key]) => key !== selectedEndpoint)
          )),
    }))
  }

  const updateText = (value: string) => {
    const custom = { mode: 'custom' as const }
    if (selectedEndpoint === 'embeddings') {
      updateDraft({ ...custom, input: value })
      return
    }
    if (selectedEndpoint === 'image-generation') {
      updateDraft({ ...custom, prompt: value })
      return
    }
    updateDraft({ ...custom, content: value })
  }

  const updateRerank = (query: string, documents: string[]) => {
    updateDraft({ mode: 'custom', query, documents })
  }

  const handleSourceChange = (value: string | null) => {
    const nextSource = (value ?? 'inherit') as ChannelTestContentSource
    if (nextSource === 'inherit') {
      updateDraft(undefined)
      return
    }
    if (nextSource === 'builtin') {
      updateDraft({ mode: 'builtin' })
      return
    }
    const fallback =
      savedOverrides[selectedEndpoint] ?? builtinOverrides[selectedEndpoint]
    if (selectedEndpoint === 'jina-rerank') {
      updateRerank(fallback?.query ?? '', fallback?.documents ?? [''])
      return
    }
    updateText(textValue(fallback))
  }

  const handleSave = () => {
    if (!canWrite || validationMessage || !currentOverride) return
    const nextSaved = { ...savedOverrides }
    const session = draftOverrides[selectedEndpoint]
    if (!session || session.mode === 'builtin') {
      delete nextSaved[selectedEndpoint]
    } else {
      const { mode: _mode, ...persistent } = session
      nextSaved[selectedEndpoint] = persistent
    }
    const nextSessionOverrides = { ...draftOverrides }
    delete nextSessionOverrides[selectedEndpoint]
    saveMutation.mutate({
      version: configQuery.data?.version ?? 1,
      overrides: nextSaved,
      endpoint: selectedEndpoint,
      sessionOverrides: nextSessionOverrides,
    })
  }

  const handleApply = () => {
    if (validationMessage || loading) return
    onApply(draftOverrides)
    onOpenChange(false)
  }

  const handleMoveDocument = (index: number, direction: -1 | 1) => {
    const nextIndex = index + direction
    if (nextIndex < 0 || nextIndex >= rerankDocuments.length) return
    const documents = [...rerankDocuments]
    ;[documents[index], documents[nextIndex]] = [
      documents[nextIndex],
      documents[index],
    ]
    setDocumentIds((previous) => {
      const next = [...previous]
      ;[next[index], next[nextIndex]] = [next[nextIndex], next[index]]
      return next
    })
    updateRerank(currentOverride?.query ?? '', documents)
  }

  let contentLabel = t('Content')
  if (selectedEndpoint === 'embeddings') {
    contentLabel = t('Input')
  } else if (selectedEndpoint === 'image-generation') {
    contentLabel = t('Prompt')
  }
  const textContent = textValue(visibleOverride)
  let contentEditor: React.ReactNode = null
  if (loading) {
    contentEditor = (
      <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
    )
  } else if (configQuery.isError) {
    contentEditor = (
      <p role='alert' className='text-destructive text-sm'>
        {configQuery.error instanceof Error
          ? configQuery.error.message
          : t('Failed to load test content')}
      </p>
    )
  } else if (source === 'custom' && selectedEndpoint === 'jina-rerank') {
    contentEditor = (
      <div className='space-y-3'>
        <div className='grid gap-2'>
          <Label htmlFor='test-rerank-query'>{t('Query')}</Label>
          <Textarea
            id='test-rerank-query'
            value={currentOverride?.query ?? ''}
            onChange={(event) =>
              updateRerank(event.target.value, rerankDocuments)
            }
            maxLength={MAX_CONTENT_LENGTH}
          />
          <p className='text-muted-foreground text-xs'>
            {t('{{count}}/4096 characters', {
              count: characterCount(currentOverride?.query ?? ''),
            })}
          </p>
        </div>
        <div className='space-y-2'>
          <Label>{t('Documents')}</Label>
          {rerankDocuments.map((document, index) => (
            <div
              key={documentIds[index] ?? `document-${index}`}
              className='flex items-start gap-2'
            >
              <div className='min-w-0 flex-1 space-y-1'>
                <Textarea
                  value={document}
                  onChange={(event) => {
                    const documents = [...rerankDocuments]
                    documents[index] = event.target.value
                    updateRerank(currentOverride?.query ?? '', documents)
                  }}
                  maxLength={MAX_CONTENT_LENGTH}
                  aria-label={t('Document {{index}}', { index: index + 1 })}
                />
                <p className='text-muted-foreground text-xs'>
                  {t('{{count}}/4096 characters', {
                    count: characterCount(document),
                  })}
                </p>
              </div>
              <div className='flex shrink-0 flex-col gap-1'>
                <Button
                  type='button'
                  variant='outline'
                  size='icon'
                  onClick={() => handleMoveDocument(index, -1)}
                  disabled={index === 0}
                  aria-label={t('Move document up')}
                >
                  <ArrowUp className='size-4' />
                </Button>
                <Button
                  type='button'
                  variant='outline'
                  size='icon'
                  onClick={() => handleMoveDocument(index, 1)}
                  disabled={index === rerankDocuments.length - 1}
                  aria-label={t('Move document down')}
                >
                  <ArrowDown className='size-4' />
                </Button>
                <Button
                  type='button'
                  variant='outline'
                  size='icon'
                  onClick={() => {
                    setDocumentIds((previous) =>
                      previous.filter(
                        (_, documentIndex) => documentIndex !== index
                      )
                    )
                    updateRerank(
                      currentOverride?.query ?? '',
                      rerankDocuments.filter(
                        (_, documentIndex) => documentIndex !== index
                      )
                    )
                  }}
                  disabled={rerankDocuments.length === 1}
                  aria-label={t('Remove document')}
                >
                  <Minus className='size-4' />
                </Button>
              </div>
            </div>
          ))}
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => {
              setDocumentIds((previous) => [
                ...previous,
                createDocumentId(documentIdCounter.current),
              ])
              updateRerank(currentOverride?.query ?? '', [
                ...rerankDocuments,
                '',
              ])
            }}
            disabled={rerankDocuments.length >= MAX_DOCUMENTS}
          >
            <Plus className='mr-1 size-4' />
            {t('Add document')}
          </Button>
        </div>
      </div>
    )
  } else if (source === 'custom') {
    contentEditor = (
      <div className='grid gap-2'>
        <Label htmlFor='test-content-value'>{contentLabel}</Label>
        <Textarea
          id='test-content-value'
          value={textValue(currentOverride)}
          onChange={(event) => updateText(event.target.value)}
          maxLength={MAX_CONTENT_LENGTH}
        />
        <p className='text-muted-foreground text-xs'>
          {t('{{count}}/4096 characters', {
            count: characterCount(textValue(currentOverride)),
          })}
        </p>
      </div>
    )
  } else {
    contentEditor = (
      <div className='bg-muted/40 rounded-md border p-3 text-sm'>
        <p className='text-muted-foreground mb-1 text-xs'>
          {t('Current effective preset')}
        </p>
        <p className='break-words whitespace-pre-wrap'>
          {selectedEndpoint === 'jina-rerank'
            ? `${visibleOverride?.query ?? ''}\n${visibleOverride?.documents?.join('\n') ?? ''}`
            : textContent}
        </p>
      </div>
    )
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className='flex w-full flex-col sm:max-w-xl'>
        <SheetHeader>
          <SheetTitle>{t('Test Content')}</SheetTitle>
          <SheetDescription>
            {t('Parameter overrides are applied after test content.')}
          </SheetDescription>
        </SheetHeader>
        <div className='min-h-0 flex-1 space-y-4 overflow-y-auto px-4 pb-4'>
          <div className='grid gap-2'>
            <Label htmlFor='test-content-endpoint'>{t('Endpoint Type')}</Label>
            {isAutoEndpoint ? (
              <>
                <Select
                  value={selectedEndpoint}
                  onValueChange={(value) => {
                    if (value && isEndpoint(value)) setSelectedEndpoint(value)
                  }}
                >
                  <SelectTrigger id='test-content-endpoint'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {endpoints.map((endpoint) => (
                      <SelectItem key={endpoint} value={endpoint}>
                        {endpoint}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Each model uses the configuration for its backend-resolved endpoint.'
                  )}
                </p>
              </>
            ) : (
              <p className='bg-muted/40 rounded-md border px-3 py-2 text-sm'>
                {endpointType}
              </p>
            )}
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='test-content-source'>
              {t('Test content source')}
            </Label>
            <Select
              value={source}
              onValueChange={handleSourceChange}
              disabled={loading}
            >
              <SelectTrigger id='test-content-source'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='inherit'>
                  {t('Use channel default')}
                </SelectItem>
                <SelectItem value='builtin'>
                  {t('Use built-in preset for this test')}
                </SelectItem>
                <SelectItem value='custom'>
                  {t('Use custom content for this test')}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          {contentEditor}
          {validationMessage && (
            <p role='alert' className='text-destructive text-sm'>
              {t(validationMessage)}
            </p>
          )}
        </div>
        <SheetFooter className='flex-row justify-end gap-2 border-t px-4 py-3'>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          {canWrite && (
            <Button
              variant='outline'
              onClick={handleSave}
              disabled={
                saving ||
                loading ||
                source === 'inherit' ||
                Boolean(validationMessage)
              }
            >
              {t('Save as channel default')}
            </Button>
          )}
          <Button
            onClick={handleApply}
            disabled={
              loading || configQuery.isError || Boolean(validationMessage)
            }
          >
            {t('Apply to this test')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

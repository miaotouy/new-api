import { ArrowDown, ArrowUp, GripVertical, Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type { ApiKeyRouteOption, ApiKeyRouteRule } from '../types'

type TokenRouteEditorProps = {
  groups: string[]
  channels: ApiKeyRouteOption[]
  items: ApiKeyRouteRule[]
  onChange: (items: ApiKeyRouteRule[]) => void
  disabled?: boolean
}

export function TokenRouteEditor(props: TokenRouteEditorProps) {
  const { t } = useTranslation()
  const [kind, setKind] = useState<'group' | 'channel'>('group')
  const [selectedValue, setSelectedValue] = useState('')
  const [draggedIndex, setDraggedIndex] = useState<number | null>(null)

  const availableChannels = useMemo(
    () => props.channels.filter((channel) => channel.status === 1),
    [props.channels]
  )

  const addItem = () => {
    if (!selectedValue) return
    if (kind === 'group') {
      if (
        props.items.some(
          (item) => item.kind === 'group' && item.group === selectedValue
        )
      ) {
        return
      }
      props.onChange([...props.items, { kind, group: selectedValue }])
    } else {
      const channel = availableChannels.find(
        (option) => String(option.id) === selectedValue
      )
      if (
        !channel ||
        props.items.some((item) => item.channel_id === channel.id)
      ) {
        return
      }
      props.onChange([
        ...props.items,
        { kind, group: channel.group, channel_id: channel.id },
      ])
    }
    setSelectedValue('')
  }

  const moveItem = (index: number, target: number) => {
    if (index === target || target < 0 || target >= props.items.length) return
    const next = [...props.items]
    const [item] = next.splice(index, 1)
    next.splice(target, 0, item)
    props.onChange(next)
  }

  return (
    <div className='flex flex-col gap-3 rounded-lg border p-3'>
      <div className='flex flex-col gap-2 sm:flex-row'>
        <Select
          value={kind}
          onValueChange={(value) => {
            if (value !== 'group' && value !== 'channel') return
            setKind(value)
            setSelectedValue('')
          }}
        >
          <SelectTrigger className='w-full sm:w-32'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='group'>{t('Group route')}</SelectItem>
            <SelectItem value='channel'>{t('Channel route')}</SelectItem>
          </SelectContent>
        </Select>
        <Select
          value={selectedValue}
          onValueChange={(value) => setSelectedValue(value ?? '')}
        >
          <SelectTrigger className='min-w-0 flex-1'>
            <SelectValue placeholder={t('Select a route candidate')} />
          </SelectTrigger>
          <SelectContent>
            {kind === 'group'
              ? props.groups.map((group) => (
                  <SelectItem key={group} value={group}>
                    {group}
                  </SelectItem>
                ))
              : availableChannels.map((channel) => (
                  <SelectItem key={channel.id} value={String(channel.id)}>
                    {channel.name} · {channel.group} · ×{channel.group_ratio} · #{channel.id}
                  </SelectItem>
                ))}
          </SelectContent>
        </Select>
        <Button
          type='button'
          variant='outline'
          size='icon'
          onClick={addItem}
          disabled={props.disabled || !selectedValue}
          title={t('Add route candidate')}
          aria-label={t('Add route candidate')}
        >
          <Plus />
        </Button>
      </div>

      {props.items.length === 0 ? (
        <p className='text-muted-foreground text-xs'>
          {t('No manual route candidates configured')}
        </p>
      ) : (
        <ol className='flex flex-col gap-2'>
          {props.items.map((item, index) => {
            const channel =
              item.kind === 'channel'
                ? props.channels.find(
                    (option) =>
                      option.id === item.channel_id && option.group === item.group
                  )
                : undefined
            const label =
              item.kind === 'group'
                ? `${t('Group')}: ${item.group}`
                : `${t('Channel')}: ${channel?.name || `#${item.channel_id}`} · ${item.group}`
            return (
              <li
                key={`${item.kind}-${item.group}-${item.channel_id ?? index}`}
                className='bg-muted/40 flex items-center gap-2 rounded-md px-2 py-1.5 text-sm'
                onDragOver={(event) => event.preventDefault()}
                onDrop={() => {
                  if (draggedIndex !== null) moveItem(draggedIndex, index)
                  setDraggedIndex(null)
                }}
              >
                <span
                  className='text-muted-foreground cursor-grab touch-none active:cursor-grabbing'
                  draggable={!props.disabled}
                  onDragStart={() => setDraggedIndex(index)}
                  onDragEnd={() => setDraggedIndex(null)}
                  aria-label={t('Manual fallback order')}
                >
                  <GripVertical className='size-4' />
                </span>
                <span className='text-muted-foreground w-5 text-center text-xs tabular-nums'>
                  {index + 1}
                </span>
                <div className='min-w-0 flex-1'>
                  <div className='truncate'>{label}</div>
                  {item.kind === 'group' ? (
                    <div className='text-muted-foreground text-xs'>
                      {t('Group ratios')}: ×
                      {props.channels.find((option) => option.group === item.group)
                        ?.group_ratio ?? 1}
                    </div>
                  ) : channel ? (
                    <div className='text-muted-foreground truncate text-xs'>
                      {channel.status !== 1 ? `${t('Disabled')} · ` : ''}
                      {t('Models')}: {channel.models || '-'} · ×
                      {channel.group_ratio}
                    </div>
                  ) : (
                    <div className='text-destructive text-xs'>{t('Disabled')}</div>
                  )}
                </div>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-xs'
                  onClick={() => moveItem(index, index - 1)}
                  disabled={props.disabled || index === 0}
                  title={t('Move route up')}
                  aria-label={t('Move route up')}
                >
                  <ArrowUp />
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-xs'
                  onClick={() => moveItem(index, index + 1)}
                  disabled={props.disabled || index === props.items.length - 1}
                  title={t('Move route down')}
                  aria-label={t('Move route down')}
                >
                  <ArrowDown />
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon-xs'
                  onClick={() =>
                    props.onChange(
                      props.items.filter((_, itemIndex) => itemIndex !== index)
                    )
                  }
                  disabled={props.disabled}
                  title={t('Remove route candidate')}
                  aria-label={t('Remove route candidate')}
                >
                  <Trash2 />
                </Button>
              </li>
            )
          })}
        </ol>
      )}
    </div>
  )
}

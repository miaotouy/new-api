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
import { ArrowDown, ArrowUp, GripVertical, Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import type { ApiKeyRouteOption, ApiKeyRouteRule } from '../types'
import {
  RouteSelectCombobox,
  type RouteSelectOption,
} from './route-select-combobox'

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

  const kindOptions = useMemo<RouteSelectOption[]>(
    () => [
      { value: 'group', label: t('Group route') },
      { value: 'channel', label: t('Channel route') },
    ],
    [t]
  )

  const candidateOptions = useMemo<RouteSelectOption[]>(() => {
    if (kind === 'group') {
      return props.groups.map((group) => ({ value: group, label: group }))
    }

    return availableChannels.map((channel) => ({
      value: String(channel.id),
      label: `${channel.name} · ${channel.group} · ×${channel.group_ratio} · #${channel.id}`,
    }))
  }, [availableChannels, kind, props.groups])

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
        <RouteSelectCombobox
          options={kindOptions}
          value={kind}
          onValueChange={(value) => {
            if (value !== 'group' && value !== 'channel') return
            setKind(value)
            setSelectedValue('')
          }}
          disabled={props.disabled}
          className='w-full sm:w-32'
        />
        <RouteSelectCombobox
          options={candidateOptions}
          value={selectedValue}
          onValueChange={setSelectedValue}
          placeholder={t('Select a route candidate')}
          searchable
          disabled={props.disabled}
          className='flex-1'
        />
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
                      option.id === item.channel_id &&
                      option.group === item.group
                  )
                : undefined
            const label =
              item.kind === 'group'
                ? `${t('Group')}: ${item.group}`
                : `${t('Channel')}: ${channel?.name || `#${item.channel_id}`} · ${item.group}`
            let routeDetails = (
              <div className='text-destructive text-xs'>{t('Disabled')}</div>
            )
            if (item.kind === 'group') {
              routeDetails = (
                <div className='text-muted-foreground text-xs'>
                  {t('Group ratios')}: ×
                  {props.channels.find((option) => option.group === item.group)
                    ?.group_ratio ?? 1}
                </div>
              )
            } else if (channel) {
              routeDetails = (
                <div className='text-muted-foreground truncate text-xs'>
                  {channel.status !== 1 ? `${t('Disabled')} · ` : ''}
                  {t('Models')}: {channel.models || '-'} · ×
                  {channel.group_ratio}
                </div>
              )
            }
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
                  {routeDetails}
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

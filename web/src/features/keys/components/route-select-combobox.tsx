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
import { UnfoldMoreIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'

export type RouteSelectOption = {
  value: string
  label: string
}

type RouteSelectComboboxProps = {
  options: RouteSelectOption[]
  value: string
  onValueChange: (value: string) => void
  placeholder?: string
  searchable?: boolean
  disabled?: boolean
  className?: string
  id?: string
  'aria-describedby'?: string
  'aria-invalid'?: boolean
  'data-form-root'?: string
}

export function RouteSelectCombobox(props: RouteSelectComboboxProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [searchValue, setSearchValue] = useState('')
  const selectedOption = props.options.find(
    (option) => option.value === props.value
  )

  const filteredOptions = useMemo(() => {
    const search = searchValue.trim().toLowerCase()
    if (!search) return props.options

    return props.options.filter(
      (option) =>
        option.label.toLowerCase().includes(search) ||
        option.value.toLowerCase().includes(search)
    )
  }, [props.options, searchValue])

  const handleSelect = (selectedValue: string) => {
    props.onValueChange(selectedValue)
    setOpen(false)
    setSearchValue('')
  }

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen)
        if (!nextOpen) setSearchValue('')
      }}
    >
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            role='combobox'
            aria-expanded={open}
            disabled={props.disabled}
            id={props.id}
            aria-describedby={props['aria-describedby']}
            aria-invalid={props['aria-invalid']}
            data-form-root={props['data-form-root']}
            className={cn(
              'border-input min-w-0 shrink justify-between gap-1.5 bg-transparent pr-2 pl-2.5 font-normal',
              props.className
            )}
          />
        }
      >
        <span
          className={cn(
            'min-w-0 flex-1 truncate text-left',
            !selectedOption && 'text-muted-foreground'
          )}
        >
          {selectedOption?.label || props.placeholder}
        </span>
        <HugeiconsIcon
          icon={UnfoldMoreIcon}
          strokeWidth={2}
          className='text-muted-foreground pointer-events-none size-4 shrink-0'
        />
      </PopoverTrigger>
      <PopoverContent
        align='start'
        className='w-[var(--anchor-width)] overflow-hidden rounded-lg p-0'
        onWheel={(event) => event.stopPropagation()}
        onTouchMove={(event) => event.stopPropagation()}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <Command shouldFilter={false}>
          {props.searchable && (
            <CommandInput
              placeholder={t('Search...')}
              value={searchValue}
              onValueChange={setSearchValue}
            />
          )}
          <CommandList>
            <CommandEmpty>{t('No results found')}</CommandEmpty>
            <CommandGroup>
              {filteredOptions.map((option) => (
                <CommandItem
                  key={option.value}
                  value={option.value}
                  data-checked={
                    option.value === props.value ? 'true' : undefined
                  }
                  onSelect={() => handleSelect(option.value)}
                >
                  <span className='min-w-0 flex-1 truncate'>
                    {option.label}
                  </span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

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
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import type { LogOtherData } from '../types'

interface RequestEndpointCellProps {
  other: LogOtherData | null
  className?: string
}

/**
 * Renders the inbound request path and, when the upstream endpoint differs,
 * the converted outbound path on a second line.
 */
export function RequestEndpointCell(props: RequestEndpointCellProps) {
  const { t } = useTranslation()
  const requestPath = props.other?.request_path
  if (!requestPath) return null

  const upstreamPath = props.other?.upstream_request_path
  if (!upstreamPath || upstreamPath === requestPath) {
    return (
      <span
        className={cn(
          'block max-w-[200px] truncate font-mono text-xs',
          props.className
        )}
        title={requestPath}
      >
        {requestPath}
      </span>
    )
  }

  return (
    <div
      className={cn(
        'flex max-w-[200px] min-w-0 flex-col gap-0.5 font-mono text-xs',
        props.className
      )}
    >
      <span
        className='min-w-0 truncate'
        title={`${t('Inbound')}: ${requestPath}`}
      >
        <span className='text-muted-foreground'>{t('Inbound')}: </span>
        {requestPath}
      </span>
      <span
        className='min-w-0 truncate'
        title={`${t('Outbound')}: ${upstreamPath}`}
      >
        <span className='text-muted-foreground'>{t('Outbound')}: </span>
        {upstreamPath}
      </span>
    </div>
  )
}

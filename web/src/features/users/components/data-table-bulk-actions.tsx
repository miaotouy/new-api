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
import type { Table } from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { formatNumber } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { AuthOperationError } from '@/lib/secure-verification'

import { disableUsers } from '../api'
import type { User } from '../types'
import { useUsers } from './users-provider'

interface DataTableBulkActionsProps {
  table: Table<User>
}

export function DataTableBulkActions({ table }: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const { requestVerification, triggerRefresh } = useUsers()
  const [busy, setBusy] = useState(false)

  const selected = table
    .getFilteredSelectedRowModel()
    .rows.filter((row) => row.getCanSelect())

  async function disableSelected() {
    const ids = selected.map((row) => row.original.id)
    if (!ids.length || busy) return
    setBusy(true)
    try {
      const proof = await requestVerification({
        scope: 'admin.user.manage',
        context: { action: 'batch_disable', user_ids: ids },
        title: t('Disable selected users'),
        description: t('Verify to disable the selected accounts ({{total}}).', {
          total: formatNumber(ids.length),
        }),
      })
      if (!proof) return
      try {
        const result = await disableUsers(ids, proof.proof_token)
        if (!result.success) {
          handleServerError(result, t('Batch disable failed'))
          return
        }
        toast.success(
          t('Accounts disabled: {{total}}', {
            total: formatNumber(result.data),
          })
        )
        table.resetRowSelection()
      } finally {
        triggerRefresh()
      }
    } catch (error) {
      handleServerError(
        AuthOperationError.from(error),
        t('Batch disable failed')
      )
    } finally {
      setBusy(false)
    }
  }

  return (
    <BulkActionsToolbar table={table} entityName={t('User')}>
      <Button
        variant='destructive'
        size='sm'
        onClick={disableSelected}
        disabled={busy || selected.length === 0}
      >
        {t('Disable selected users')}
      </Button>
    </BulkActionsToolbar>
  )
}

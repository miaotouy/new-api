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

import dayjs from 'dayjs'
import { Download, FileJson, FileSpreadsheet } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

import {
  downloadDashboardExport,
  type DashboardExportDataset,
  type DashboardExportFormat,
} from '../../lib/export'

interface DashboardExportButtonProps {
  filename: string
  disabled?: boolean
  getDatasets: () => DashboardExportDataset[]
}

export function DashboardExportButton(props: DashboardExportButtonProps) {
  const { t } = useTranslation()

  function handleExport(format: DashboardExportFormat) {
    try {
      const datasets = props.getDatasets()
      if (!datasets.some((dataset) => dataset.rows.length > 0)) {
        toast.info(t('No data available'))
        return
      }
      const filename = `${props.filename}-${dayjs().format(
        'YYYY-MM-DD_HH-mm-ss'
      )}.${format}`
      downloadDashboardExport(datasets, format, t('Dataset'), filename)
    } catch {
      toast.error(t('Failed to export data. Please try again.'))
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={props.disabled}
        render={
          <Button
            variant='outline'
            size='sm'
            className='h-8 shrink-0'
            title={t('Export current data')}
          />
        }
      >
        <Download data-icon='inline-start' />
        {t('Export')}
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end'>
        <DropdownMenuItem onClick={() => handleExport('csv')}>
          <FileSpreadsheet />
          {t('Export as CSV')}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => handleExport('json')}>
          <FileJson />
          {t('Export as JSON')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

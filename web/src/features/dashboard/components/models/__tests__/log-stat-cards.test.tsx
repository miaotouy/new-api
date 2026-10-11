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
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/http-client'

import { LogStatCards } from '../log-stat-cards'

afterEach(() => vi.restoreAllMocks())

describe('model token statistics', () => {
  it('shows the same normalized total as the token chart instead of legacy quota tokens', async () => {
    vi.spyOn(api, 'get')
      .mockResolvedValueOnce({
        data: {
          success: true,
          data: [{ quota: 0, count: 1, token_used: 120 }],
        },
      })
      .mockResolvedValueOnce({
        data: {
          success: true,
          data: [
            {
              created_at: 1,
              input_tokens: 300,
              output_tokens: 20,
              cached_tokens: 200,
              token_used: 320,
            },
          ],
        },
      })
    render(<LogStatCards />)
    expect(await screen.findByText('320')).toBeVisible()
  })
  it('shows an inconsistent historical cache rate above 100 percent', async () => {
    vi.spyOn(api, 'get')
      .mockResolvedValueOnce({
        data: {
          success: true,
          data: [{ quota: 0, count: 1, token_used: 120 }],
        },
      })
      .mockResolvedValueOnce({
        data: {
          success: true,
          data: [
            {
              created_at: 1,
              input_tokens: 100,
              output_tokens: 20,
              cached_tokens: 120,
              token_used: 120,
            },
          ],
        },
      })
    render(<LogStatCards />)
    expect(await screen.findByText(/120\.0%/)).toBeVisible()
  })
})

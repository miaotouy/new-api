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
import { describe, expect, test } from 'vitest'

import { getCacheHitRate } from '../format'

describe('cache hit rate', () => {
  test('calculates cached input as a percentage of total input', () => {
    expect(getCacheHitRate(38272, 39961)).toBeCloseTo(95.7734, 4)
  })

  test('returns null when there is no valid cache or input token count', () => {
    expect(getCacheHitRate(0, 39961)).toBeNull()
    expect(getCacheHitRate(100, 0)).toBeNull()
    expect(getCacheHitRate(Number.NaN, 100)).toBeNull()
  })

  test('keeps malformed ratios within the display range', () => {
    expect(getCacheHitRate(500, 100)).toBe(100)
    expect(getCacheHitRate(-1, 100)).toBeNull()
  })
})

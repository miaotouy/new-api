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
  test.each([
    {
      name: 'legacy Claude logs include cache reads and writes in total input',
      cachedTokens: 105913,
      inputTokens: 2,
      other: { claude: true, cache_creation_tokens: 1047 },
      expected: 99.0193,
    },
    {
      name: 'Claude split cache writes take precedence over the aggregate',
      cachedTokens: 600,
      inputTokens: 200,
      other: {
        claude: true,
        cache_creation_tokens: 300,
        cache_creation_tokens_5m: 120,
        cache_creation_tokens_1h: 80,
      },
      expected: 60,
    },
    {
      name: 'Anthropic usage semantics include cache tokens without a Claude flag',
      cachedTokens: 600,
      inputTokens: 200,
      other: { usage_semantic: 'anthropic', cache_creation_tokens: 200 },
      expected: 60,
    },
    {
      name: 'OpenAI usage semantics keep inclusive input despite a Claude flag',
      cachedTokens: 600,
      inputTokens: 1000,
      other: {
        usage_semantic: 'openai',
        claude: true,
        cache_creation_tokens: 200,
      },
      expected: 60,
    },
    {
      name: 'Claude with zero uncached input still includes cache writes',
      cachedTokens: 800,
      inputTokens: 0,
      other: { claude: true, cache_creation_tokens: 200 },
      expected: 80,
    },
    {
      name: 'Claude with only cache reads has a full cache hit rate',
      cachedTokens: 800,
      inputTokens: 0,
      other: { claude: true },
      expected: 100,
    },
  ])('$name', ({ cachedTokens, inputTokens, other, expected }) => {
    expect(getCacheHitRate(cachedTokens, inputTokens, other)).toBeCloseTo(
      expected,
      4
    )
  })

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

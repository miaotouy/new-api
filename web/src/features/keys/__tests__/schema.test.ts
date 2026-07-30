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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { apiKeySchema } from '../types'

const legacyApiKey = {
  id: 1,
  name: 'legacy key',
  key: 'sk-****',
  status: 1,
  remain_quota: 0,
  used_quota: 0,
  unlimited_quota: true,
  expired_time: -1,
  created_time: 1,
  accessed_time: 1,
  group: 'default',
  cross_group_retry: false,
  model_limits_enabled: false,
  model_limits: '',
  allow_ips: '',
  max_ratio: 0,
  failover_enabled: false,
  rate_limit: 0,
  rate_limit_window_seconds: 0,
}

describe('API key response schema', () => {
  test('normalizes empty legacy routing fields to current defaults', () => {
    const parsed = apiKeySchema.parse({
      ...legacyApiKey,
      route_mode: '',
      auto_route_strategy: '',
    })

    assert.equal(parsed.route_mode, 'auto')
    assert.equal(parsed.auto_route_strategy, 'priority')
  })

  test('keeps rejecting unknown routing values', () => {
    assert.throws(() =>
      apiKeySchema.parse({
        ...legacyApiKey,
        route_mode: 'random',
        auto_route_strategy: 'latency',
      })
    )
  })
})

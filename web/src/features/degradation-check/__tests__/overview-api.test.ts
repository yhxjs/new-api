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
import { afterEach, expect, test } from 'vitest'

import { api } from '@/lib/api'

import { getDegradationOverview } from '../api'

const originalAdapter = api.defaults.adapter

afterEach(() => {
  api.defaults.adapter = originalAdapter
})

test('HTTP success with a failed overview envelope rejects with the server error', async () => {
  api.defaults.adapter = async (config) => ({
    config,
    data: { success: false, message: 'Overview storage unavailable' },
    headers: {},
    status: 200,
    statusText: 'OK',
  })

  await expect(getDegradationOverview()).rejects.toThrow(
    'Overview storage unavailable'
  )
})

test('a failed overview without a message still rejects with a useful fallback', async () => {
  api.defaults.adapter = async (config) => ({
    config,
    data: { success: false, message: '' },
    headers: {},
    status: 200,
    statusText: 'OK',
  })

  await expect(getDegradationOverview()).rejects.toThrow(
    'Failed to load degradation data.'
  )
})

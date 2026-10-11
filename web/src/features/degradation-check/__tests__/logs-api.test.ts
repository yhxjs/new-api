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

import { getDegradationLogs } from '../api'

const originalAdapter = api.defaults.adapter

afterEach(() => {
  api.defaults.adapter = originalAdapter
})

test.each([
  ['Log storage unavailable', 'Log storage unavailable'],
  ['', 'Failed to load logs'],
])('a failed logs envelope with message %j rejects', async (message, error) => {
  api.defaults.adapter = async (config) => ({
    config,
    data: { success: false, message },
    headers: {},
    status: 200,
    statusText: 'OK',
  })

  await expect(getDegradationLogs({ channel_id: 1 })).rejects.toThrow(error)
})

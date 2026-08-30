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

import { parseHeaderNavModules, serializeHeaderNavModules } from '../config'

describe('header navigation settings config', () => {
  test('defaults the user leaderboard to disabled for empty and legacy config', () => {
    expect(parseHeaderNavModules('').rankings.userLeaderboardEnabled).toBe(
      false
    )
    expect(
      parseHeaderNavModules('{"rankings":{"enabled":true,"requireAuth":false}}')
        .rankings.userLeaderboardEnabled
    ).toBe(false)
  })

  test('preserves an explicitly enabled user leaderboard when serialized', () => {
    const parsed = parseHeaderNavModules(
      '{"rankings":{"enabled":true,"requireAuth":false,"userLeaderboardEnabled":true}}'
    )
    const roundTrip = parseHeaderNavModules(serializeHeaderNavModules(parsed))

    expect(roundTrip.rankings.userLeaderboardEnabled).toBe(true)
  })
})

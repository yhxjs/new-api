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

import {
  isUserLeaderboardEnabledFromStatus,
  parseHeaderNavModules,
} from '../nav-modules'

describe('header navigation module parsing', () => {
  test('keeps model status disabled and authenticated for missing and legacy settings', () => {
    expect(parseHeaderNavModules('').modelStatus).toEqual({
      enabled: false,
      requireAuth: true,
    })
    expect(
      parseHeaderNavModules(
        JSON.stringify({
          modelStatus: { enabled: true, requireAuth: false },
        })
      ).modelStatus
    ).toEqual({
      enabled: true,
      requireAuth: true,
    })
  })

  test('keeps the user leaderboard disabled for missing and legacy settings', () => {
    expect(parseHeaderNavModules('').rankings).toEqual({
      enabled: true,
      requireAuth: false,
      userLeaderboardEnabled: false,
    })
    expect(
      parseHeaderNavModules(
        JSON.stringify({ rankings: { enabled: true, requireAuth: false } })
      ).rankings
    ).toEqual({
      enabled: true,
      requireAuth: false,
      userLeaderboardEnabled: false,
    })
  })

  test('reads an explicitly enabled user leaderboard from status', () => {
    const status = {
      HeaderNavModules: JSON.stringify({
        rankings: {
          enabled: true,
          requireAuth: false,
          userLeaderboardEnabled: true,
        },
      }),
    }

    expect(isUserLeaderboardEnabledFromStatus(status)).toBe(true)
  })
})

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

import { placeShareLabels } from '../user-label-layout'

describe('user model share label layout', () => {
  test('keeps separated labels on the first lane', () => {
    expect(
      placeShareLabels(400, [
        { anchor: 0.2, width: 40 },
        { anchor: 0.7, width: 40 },
      ])
    ).toEqual([
      { left: 60, lane: 0 },
      { left: 260, lane: 0 },
    ])
  })

  test('moves colliding labels onto separate lanes', () => {
    expect(
      placeShareLabels(200, [
        { anchor: 0.45, width: 100 },
        { anchor: 0.55, width: 100 },
      ])
    ).toEqual([
      { left: 40, lane: 0 },
      { left: 60, lane: 1 },
    ])
  })

  test('clamps labels and oversized measurements inside the container', () => {
    expect(
      placeShareLabels(100, [
        { anchor: 0, width: 40 },
        { anchor: 0.5, width: 140 },
        { anchor: 1, width: 40 },
      ])
    ).toEqual([
      { left: 0, lane: 0 },
      { left: 0, lane: 1 },
      { left: 60, lane: 0 },
    ])
  })

  test('uses anchor order for placement while preserving input order', () => {
    expect(
      placeShareLabels(400, [
        { anchor: 0.8, width: 40 },
        { anchor: 0.2, width: 40 },
        { anchor: 0.21, width: 40 },
      ])
    ).toEqual([
      { left: 300, lane: 0 },
      { left: 60, lane: 0 },
      { left: 64, lane: 1 },
    ])
  })
})

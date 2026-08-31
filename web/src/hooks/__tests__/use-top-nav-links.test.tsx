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
import { renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { useTopNavLinks } from '../use-top-nav-links'

const mocks = vi.hoisted(() => ({
  status: null as Record<string, unknown> | null,
  user: null as { id: number } | null,
}))

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({ status: mocks.status }),
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: () => ({ auth: { user: mocks.user } }),
}))

describe('top navigation model status link', () => {
  beforeEach(() => {
    mocks.status = null
    mocks.user = null
  })

  test('hides model status when the module is disabled', () => {
    mocks.user = { id: 1 }

    const { result } = renderHook(() => useTopNavLinks())

    expect(result.current.map((link) => link.href)).not.toContain(
      '/model-status'
    )
  })

  test('hides model status from unauthenticated visitors when enabled', () => {
    mocks.status = {
      HeaderNavModules: JSON.stringify({
        modelStatus: { enabled: true, requireAuth: false },
      }),
    }

    const { result } = renderHook(() => useTopNavLinks())

    expect(result.current.map((link) => link.href)).not.toContain(
      '/model-status'
    )
  })

  test('places model status immediately after rankings for authenticated users', () => {
    mocks.status = {
      HeaderNavModules: JSON.stringify({
        rankings: { enabled: true, requireAuth: false },
        modelStatus: { enabled: true, requireAuth: false },
      }),
    }
    mocks.user = { id: 1 }

    const { result } = renderHook(() => useTopNavLinks())

    expect(result.current.map((link) => link.href)).toEqual([
      '/',
      '/dashboard',
      '/pricing',
      '/rankings',
      '/model-status',
      '/docs',
      '/about',
    ])
  })
})

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
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { Route } from '../../../routes/model-status'

const mocks = vi.hoisted(() => ({
  getFreshModuleAccess: vi.fn(),
  getState: vi.fn(),
  redirect: vi.fn((options: Record<string, unknown>) =>
    Object.assign(new Error('redirect'), { options })
  ),
}))

vi.mock('@tanstack/react-router', () => ({
  createFileRoute: () => (options: Record<string, unknown>) => ({ options }),
  redirect: mocks.redirect,
}))

vi.mock('../index', () => ({
  ModelStatusPage: () => null,
}))

vi.mock('@/lib/nav-modules', () => ({
  getFreshModuleAccess: mocks.getFreshModuleAccess,
}))

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: { getState: mocks.getState },
}))

type BeforeLoad = (args: { location: { href: string } }) => Promise<unknown>

function getBeforeLoad(): BeforeLoad {
  return (
    Route as unknown as {
      options: { beforeLoad: BeforeLoad }
    }
  ).options.beforeLoad
}

describe('model status route access', () => {
  beforeEach(() => {
    mocks.getFreshModuleAccess.mockReset()
    mocks.getState.mockReset()
    mocks.redirect.mockClear()
  })

  test('redirects to home when the page is disabled', async () => {
    mocks.getFreshModuleAccess.mockResolvedValue({
      enabled: false,
      requireAuth: true,
    })

    await expect(
      getBeforeLoad()({ location: { href: '/model-status' } })
    ).rejects.toThrow('redirect')
    expect(mocks.redirect).toHaveBeenCalledWith({ to: '/' })
    expect(mocks.getState).not.toHaveBeenCalled()
  })

  test('always redirects unauthenticated visitors to sign in', async () => {
    mocks.getFreshModuleAccess.mockResolvedValue({
      enabled: true,
      requireAuth: false,
    })
    mocks.getState.mockReturnValue({ auth: { user: null } })

    await expect(
      getBeforeLoad()({ location: { href: '/model-status?group=vip' } })
    ).rejects.toThrow('redirect')
    expect(mocks.redirect).toHaveBeenCalledWith({
      to: '/sign-in',
      search: { redirect: '/model-status?group=vip' },
    })
  })

  test('allows authenticated users when the page is enabled', async () => {
    mocks.getFreshModuleAccess.mockResolvedValue({
      enabled: true,
      requireAuth: false,
    })
    mocks.getState.mockReturnValue({ auth: { user: { id: 1 } } })

    await expect(
      getBeforeLoad()({ location: { href: '/model-status' } })
    ).resolves.toBeUndefined()
    expect(mocks.getFreshModuleAccess).toHaveBeenCalledWith('modelStatus')
    expect(mocks.redirect).not.toHaveBeenCalled()
  })
})

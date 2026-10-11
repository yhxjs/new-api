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

import { ROLE } from '@/lib/roles'

import { Route } from '../../../routes/degradation-check'

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
vi.mock('../index', () => ({ DegradationCheckPage: () => null }))
vi.mock('@/lib/nav-modules', () => ({
  getFreshModuleAccess: mocks.getFreshModuleAccess,
}))
vi.mock('@/stores/auth-store', () => ({
  useAuthStore: { getState: mocks.getState },
}))

type BeforeLoad = (args: { location: { href: string } }) => Promise<unknown>
const beforeLoad = (Route as unknown as { options: { beforeLoad: BeforeLoad } })
  .options.beforeLoad

describe('degradation check route access', () => {
  beforeEach(() => {
    mocks.getFreshModuleAccess.mockReset()
    mocks.getState.mockReset()
    mocks.redirect.mockClear()
  })

  test('unauthenticated visitors are sent to sign in before navigation is fetched', async () => {
    mocks.getState.mockReturnValue({ auth: { user: null } })

    await expect(
      beforeLoad({ location: { href: '/degradation-check?channel=7' } })
    ).rejects.toThrow('redirect')

    expect(mocks.redirect).toHaveBeenCalledWith({
      to: '/sign-in',
      search: { redirect: '/degradation-check?channel=7' },
    })
    expect(mocks.getFreshModuleAccess).not.toHaveBeenCalled()
  })

  test('ordinary users are redirected home when navigation is disabled', async () => {
    mocks.getState.mockReturnValue({ auth: { user: { role: ROLE.USER } } })
    mocks.getFreshModuleAccess.mockResolvedValue({ enabled: false })

    await expect(
      beforeLoad({ location: { href: '/degradation-check' } })
    ).rejects.toThrow('redirect')
    expect(mocks.redirect).toHaveBeenCalledWith({ to: '/' })
  })

  test.each([
    ['ordinary user with enabled navigation', ROLE.USER, true],
    ['administrator with disabled navigation', ROLE.ADMIN, false],
    ['root with disabled navigation', ROLE.SUPER_ADMIN, false],
  ])('%s can open the page', async (_name, role, enabled) => {
    mocks.getState.mockReturnValue({ auth: { user: { role } } })
    mocks.getFreshModuleAccess.mockResolvedValue({ enabled })

    await expect(
      beforeLoad({ location: { href: '/degradation-check' } })
    ).resolves.toBeUndefined()
    expect(mocks.getFreshModuleAccess).toHaveBeenCalledWith('degradationCheck')
    expect(mocks.redirect).not.toHaveBeenCalled()
  })
})

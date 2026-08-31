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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen } from '@testing-library/react'
import i18next from 'i18next'
import { beforeAll, describe, expect, test } from 'vitest'

import { HEADER_NAV_DEFAULT } from '../config'
import { HeaderNavigationSection } from '../header-navigation-section'

describe('header navigation settings', () => {
  beforeAll(() => {
    i18next.addResourceBundle('en', 'translation', {
      Rankings: 'Rankings',
      'Enable user leaderboard': 'Enable user leaderboard',
      'Model Status': 'Model Status',
    })
  })

  test('shows an independent model status switch', () => {
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <HeaderNavigationSection
          config={HEADER_NAV_DEFAULT}
          initialSerialized=''
        />
      </QueryClientProvider>
    )

    expect(
      screen.getByRole('switch', { name: 'Model Status' })
    ).toBeInTheDocument()
    queryClient.clear()
  })

  test('disables the user leaderboard switch only while rankings are off', () => {
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })

    render(
      <QueryClientProvider client={queryClient}>
        <HeaderNavigationSection
          config={{
            ...HEADER_NAV_DEFAULT,
            rankings: {
              ...HEADER_NAV_DEFAULT.rankings,
              enabled: false,
            },
          }}
          initialSerialized=''
        />
      </QueryClientProvider>
    )

    const childSwitch = screen.getByRole('switch', {
      name: 'Enable user leaderboard',
    })
    expect(childSwitch).toHaveAttribute('aria-disabled', 'true')

    fireEvent.click(screen.getByRole('switch', { name: 'Rankings' }))

    expect(childSwitch).not.toHaveAttribute('aria-disabled')
    queryClient.clear()
  })
})

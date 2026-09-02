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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test } from 'vitest'

import { removeCookie } from '@/lib/cookies'
import { THEME_COOKIE_KEYS } from '@/lib/theme-customization'

import {
  ThemeCustomizationProvider,
  useThemeCustomization,
} from '../theme-customization-provider'

/** Switches to the neutral base palette ("Classic") and shows the active one. */
function PresetProbe() {
  const { customization, setPreset } = useThemeCustomization()
  return (
    <button type='button' onClick={() => setPreset('default')}>
      {customization.preset}
    </button>
  )
}

function renderProvider() {
  return render(
    <ThemeCustomizationProvider>
      <PresetProbe />
    </ThemeCustomizationProvider>
  )
}

afterEach(() => {
  removeCookie(THEME_COOKIE_KEYS.preset)
  document.body.removeAttribute('data-theme-preset')
  document.body.removeAttribute('data-theme-font')
})

describe('theme preset defaults', () => {
  test('applies the Anthropic palette and its serif typography when no preset is stored', () => {
    renderProvider()

    expect(document.body).toHaveAttribute('data-theme-preset', 'anthropic')
    expect(document.body).toHaveAttribute('data-theme-font', 'serif')
  })

  test('keeps the base palette marked on the body when the classic preset is selected', async () => {
    renderProvider()

    await userEvent.click(screen.getByRole('button'))

    expect(document.body).toHaveAttribute('data-theme-preset', 'default')
    expect(document.body).toHaveAttribute('data-theme-font', 'sans')
  })

  test('restores the classic preset from its cookie after a remount', async () => {
    const firstMount = renderProvider()
    await userEvent.click(screen.getByRole('button'))
    firstMount.unmount()

    renderProvider()

    expect(document.body).toHaveAttribute('data-theme-preset', 'default')
  })
})

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
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { expect, test } from 'vitest'

import en from '@/i18n/locales/en.json'
import zh from '@/i18n/locales/zh.json'

import { TimelineStrip } from '../components/timeline-strip'

test('every hourly slot has a name identifying its probe, time and status', async () => {
  const user = userEvent.setup()
  render(
    <TimelineStrip
      probeName='Logic Probe'
      slots={[0, 1, 2, 3, 4, ...Array<number>(19).fill(0)]}
    />
  )

  expect(screen.getAllByRole('button')).toHaveLength(24)
  expect(screen.queryAllByRole('button', { name: '' })).toHaveLength(0)
  const firstSlot = screen.getByRole('button', {
    name: 'Logic Probe: 23h ago: No Data',
  })
  for (const name of [
    'Logic Probe: 22h ago: Passed (100%)',
    'Logic Probe: 21h ago: Mismatched / Degraded',
    'Logic Probe: 20h ago: Execution Error',
    'Logic Probe: 19h ago: Request Timeout',
    'Logic Probe: Current Hour: No Data',
  ]) {
    expect(screen.getByRole('button', { name })).toBeInTheDocument()
  }

  await user.tab()
  expect(firstSlot).toHaveFocus()
  expect(await screen.findByText('23h ago: No Data')).toBeVisible()
})

test.each(['en', 'zh'])(
  'slots without a probe name use the %s translations',
  async (language) => {
    const i18n = createInstance()
    await i18n.init({ lng: language, resources: { en, zh } })
    render(
      <I18nextProvider i18n={i18n}>
        <TimelineStrip slots={[...Array<number>(23).fill(0), 3]} />
      </I18nextProvider>
    )

    expect(
      screen.getByRole('button', {
        name: `${i18n.t('Current Hour')}: ${i18n.t('Execution Error')}`,
      })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', {
        name: `${i18n.t('{{count}}h ago', { count: 23 })}: ${i18n.t('No Data')}`,
      })
    ).toBeInTheDocument()
  }
)

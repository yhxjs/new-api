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
import { act, cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, describe, expect, test } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import { SettingsPageProvider } from '../../components/settings-page-context'
import {
  DegradationCheckSection,
  type ProbeConfig,
} from '../degradation-check-section'

const queryClients: QueryClient[] = []
const actionsContainers: HTMLDivElement[] = []
const regexProbe: ProbeConfig = {
  id: 'comma-probe',
  name: 'Comma response',
  prompt: 'Reply with a,b',
  keywords: ['^a,b$', '^c{1,3}$'],
  match_rule: 'regex',
  case_sensitive: false,
  max_tokens: 256,
}

function renderSettings(probes: ProbeConfig[] = [], i18nInstance = i18next) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  queryClients.push(queryClient)
  const actionsContainer = document.createElement('div')
  document.body.append(actionsContainer)
  actionsContainers.push(actionsContainer)

  render(
    <QueryClientProvider client={queryClient}>
      <SettingsPageProvider actionsContainer={actionsContainer}>
        <DegradationCheckSection
          defaultValues={{
            'degradation_setting.enabled': false,
            'degradation_setting.interval_minutes': 10,
            'degradation_setting.concurrency': 1,
            'degradation_setting.channel_jitter_ms': 100,
            'degradation_setting.timeout_seconds': 30,
            'degradation_setting.retention_days': 7,
            'degradation_setting.public_mask_topology': true,
            'degradation_setting.targets': '[]',
            'degradation_setting.probes': JSON.stringify(probes),
          }}
        />
      </SettingsPageProvider>
    </QueryClientProvider>,
    {
      wrapper: ({ children }) => (
        <I18nextProvider i18n={i18nInstance}>{children}</I18nextProvider>
      ),
    }
  )
  return actionsContainer
}

describe('probe regular expression input', () => {
  afterEach(() => {
    cleanup()
    for (const queryClient of queryClients) queryClient.clear()
    queryClients.length = 0
    for (const actionsContainer of actionsContainers) actionsContainer.remove()
    actionsContainers.length = 0
  })

  test.each([
    ['any', 'Any Match'],
    ['all', 'All Match'],
    ['regex', 'Regex'],
  ] as const)('editing a %s probe initially shows %s', async (rule, label) => {
    const user = userEvent.setup()
    renderSettings([{ ...regexProbe, match_rule: rule }])

    await user.click(screen.getByRole('button', { name: 'Edit Probe' }))

    expect(
      within(screen.getByRole('dialog', { name: 'Edit Probe' })).getByRole(
        'combobox'
      )
    ).toHaveTextContent(label)
  })

  test('the selected match rule updates when the language changes', async () => {
    const user = userEvent.setup()
    const i18nInstance = i18next.createInstance()
    await i18nInstance.init({
      lng: 'en',
      fallbackLng: 'en',
      resources: { en: { translation: {} }, zh },
    })
    renderSettings([{ ...regexProbe, match_rule: 'all' }], i18nInstance)
    await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
    const select = within(
      screen.getByRole('dialog', { name: 'Edit Probe' })
    ).getByRole('combobox')

    await act(async () => {
      await i18nInstance.changeLanguage('zh')
    })

    expect(select).toHaveTextContent(zh.translation['All Match'])
    await user.click(select)
    expect(
      screen.getByRole('option', { name: zh.translation['All Match'] })
    ).toHaveAttribute('aria-selected', 'true')
  })

  test('saving a regex probe preserves a comma inside one expression', async () => {
    const user = userEvent.setup()
    renderSettings()

    await user.click(screen.getByRole('button', { name: 'Add Custom Probe' }))
    const dialog = within(screen.getByRole('dialog', { name: 'Create Probe' }))
    await user.type(
      dialog.getByPlaceholderText('e.g. Basic Math Reasoning'),
      'Comma response'
    )
    await user.tab()
    await user.tab()
    await user.keyboard('Reply with a,b')
    await user.type(dialog.getByPlaceholderText('wordA, wordB'), '^a,b$')
    await user.click(dialog.getByRole('combobox'))
    await user.click(screen.getByRole('option', { name: 'Regex' }))
    await user.click(dialog.getByRole('button', { name: 'Save Probe' }))
    expect(
      screen.queryByRole('dialog', { name: 'Create Probe' })
    ).not.toBeInTheDocument()
    expect(screen.getByText('Comma response')).toBeInTheDocument()
    expect(screen.getByText(/^Keywords:/)).toHaveTextContent('Keywords: 1')
  })

  test('editing an existing regex probe displays each expression on its own line', async () => {
    const user = userEvent.setup()
    renderSettings([regexProbe])

    await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
    const dialog = within(screen.getByRole('dialog', { name: 'Edit Probe' }))
    expect(
      dialog.getByRole('textbox', {
        name: 'Expected Regular Expressions (one per line)',
      })
    ).toHaveValue('^a,b$\n^c{1,3}$')
  })

  test('saving multiple regex lines keeps commas inside expressions and skips blank lines', async () => {
    const user = userEvent.setup()
    renderSettings([{ ...regexProbe, keywords: [] }])

    await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
    const dialog = within(screen.getByRole('dialog', { name: 'Edit Probe' }))
    const input = dialog.getByRole('textbox', {
      name: 'Expected Regular Expressions (one per line)',
    })
    await user.click(input)
    await user.paste('\n ^a,b$ \n\n ^c{1,3}$ \n')
    await user.click(dialog.getByRole('button', { name: 'Save Probe' }))

    expect(screen.getByText(/^Keywords:/)).toHaveTextContent('Keywords: 2')
    await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
    expect(
      within(screen.getByRole('dialog', { name: 'Edit Probe' })).getByRole(
        'textbox',
        { name: 'Expected Regular Expressions (one per line)' }
      )
    ).toHaveValue('^a,b$\n^c{1,3}$')
  })

  test('switching a regex probe through keyword modes retains its expression draft', async () => {
    const user = userEvent.setup()
    renderSettings([regexProbe])

    await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
    const dialog = within(screen.getByRole('dialog', { name: 'Edit Probe' }))
    for (const mode of ['Any Match', 'All Match', 'Regex']) {
      await user.click(dialog.getByRole('combobox'))
      await user.click(screen.getByRole('option', { name: mode }))
    }
    expect(
      dialog.getByRole('textbox', {
        name: 'Expected Regular Expressions (one per line)',
      })
    ).toHaveValue('^a,b$\n^c{1,3}$')
    await user.click(dialog.getByRole('button', { name: 'Save Probe' }))
    expect(screen.getByText(/^Keywords:/)).toHaveTextContent('Keywords: 2')
  })

  test.each(['Any Match', 'All Match'])(
    'saving a %s probe continues to split keywords by comma',
    async (mode) => {
      const user = userEvent.setup()
      renderSettings([
        {
          id: 'keyword-probe',
          name: 'Keyword response',
          prompt: 'Reply with alpha and beta',
          keywords: [],
          match_rule: 'any',
          case_sensitive: false,
          max_tokens: 256,
        },
      ])

      await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
      const dialog = within(screen.getByRole('dialog', { name: 'Edit Probe' }))
      await user.click(dialog.getByRole('combobox'))
      await user.click(screen.getByRole('option', { name: mode }))
      await user.type(
        dialog.getByPlaceholderText('wordA, wordB'),
        'alpha, , beta'
      )
      await user.click(dialog.getByRole('button', { name: 'Save Probe' }))

      expect(screen.getByText(/^Keywords:/)).toHaveTextContent('Keywords: 2')
      await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
      expect(
        within(screen.getByRole('dialog', { name: 'Edit Probe' })).getByRole(
          'textbox',
          { name: 'Expected Keywords (comma-separated)' }
        )
      ).toHaveValue('alpha, beta')
    }
  )
})

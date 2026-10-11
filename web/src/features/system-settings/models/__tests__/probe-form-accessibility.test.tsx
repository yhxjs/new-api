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
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, describe, expect, test } from 'vitest'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { DegradationCheckSection } from '../degradation-check-section'

const queryClients: QueryClient[] = []

async function renderSettings() {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  queryClients.push(queryClient)
  const i18nInstance = i18next.createInstance()
  await i18nInstance.init({
    lng: 'en',
    fallbackLng: 'en',
    resources: { en: { translation: {} } },
  })

  render(
    <I18nextProvider i18n={i18nInstance}>
      <QueryClientProvider client={queryClient}>
        <SettingsPageProvider actionsContainer={null}>
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
              'degradation_setting.probes': '[]',
            }}
          />
        </SettingsPageProvider>
      </QueryClientProvider>
    </I18nextProvider>
  )
}

describe('probe form accessibility', () => {
  afterEach(() => {
    cleanup()
    for (const queryClient of queryClients) queryClient.clear()
    queryClients.length = 0
  })

  test('editing named fields preserves their values when the saved probe is reopened', async () => {
    const user = userEvent.setup()
    await renderSettings()
    await user.click(screen.getByRole('button', { name: 'Add Custom Probe' }))
    const dialog = within(screen.getByRole('dialog', { name: 'Create Probe' }))
    const textFields = [
      { name: 'Probe Name', value: 'Accessible probe' },
      { name: 'System Prompt (Optional)', value: 'Be precise' },
      { name: 'Test Prompt', value: 'Reply with ok' },
      { name: 'Expected Keywords (comma-separated)', value: 'ok, yes' },
      { name: 'Exclude Words (comma-separated)', value: 'sorry, cannot' },
    ]
    for (const field of textFields) {
      await user.type(
        dialog.getByRole('textbox', { name: field.name }),
        field.value
      )
    }
    const numericFields = [
      { name: 'Max Tokens', value: 128 },
      { name: 'Temperature', value: 0.3 },
    ]
    for (const field of numericFields) {
      const input = dialog.getByRole('spinbutton', { name: field.name })
      await user.click(input)
      await user.keyboard('{Control>}a{/Control}')
      await user.keyboard(String(field.value))
    }
    await user.click(dialog.getByRole('combobox', { name: 'Match Rule' }))
    await user.click(screen.getByRole('option', { name: 'All Match' }))
    await user.click(dialog.getByRole('checkbox', { name: 'Case Sensitive' }))
    await user.click(dialog.getByRole('button', { name: 'Save Probe' }))

    expect(
      screen.queryByRole('dialog', { name: 'Create Probe' })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
    const reopened = within(screen.getByRole('dialog', { name: 'Edit Probe' }))
    for (const field of textFields) {
      expect(reopened.getByRole('textbox', { name: field.name })).toHaveValue(
        field.value
      )
    }
    for (const field of numericFields) {
      expect(
        reopened.getByRole('spinbutton', { name: field.name })
      ).toHaveValue(field.value)
    }
    expect(
      reopened.getByRole('combobox', { name: 'Match Rule' })
    ).toHaveTextContent('All Match')
    expect(
      reopened.getByRole('checkbox', { name: 'Case Sensitive' })
    ).toBeChecked()
  })

  test('labels focus the system prompt and toggle the case checkbox, which also responds to Space', async () => {
    const user = userEvent.setup()
    await renderSettings()
    await user.click(screen.getByRole('button', { name: 'Add Custom Probe' }))
    const dialog = within(screen.getByRole('dialog', { name: 'Create Probe' }))

    await user.click(
      dialog.getByText('System Prompt (Optional)', { selector: 'label' })
    )
    expect(
      dialog.getByRole('textbox', { name: 'System Prompt (Optional)' })
    ).toHaveFocus()

    const checkbox = dialog.getByRole('checkbox', { name: 'Case Sensitive' })
    await user.click(dialog.getByText('Case Sensitive', { selector: 'label' }))
    expect(checkbox).toBeChecked()
    await user.click(dialog.getByRole('spinbutton', { name: 'Temperature' }))
    await user.tab()
    expect(checkbox).toHaveFocus()
    await user.keyboard(' ')
    expect(checkbox).not.toBeChecked()
  })

  test('probe form organizes controls into balanced pairs with full-width keywords input', async () => {
    const user = userEvent.setup()
    await renderSettings()
    await user.click(screen.getByRole('button', { name: 'Add Custom Probe' }))
    const dialog = within(screen.getByRole('dialog', { name: 'Create Probe' }))

    const matchRuleSelect = dialog.getByRole('combobox', { name: 'Match Rule' })
    const excludeWordsInput = dialog.getByRole('textbox', {
      name: 'Exclude Words (comma-separated)',
    })
    const keywordsInput = dialog.getByRole('textbox', {
      name: 'Expected Keywords (comma-separated)',
    })
    const maxTokensInput = dialog.getByRole('spinbutton', {
      name: 'Max Tokens',
    })
    const temperatureInput = dialog.getByRole('spinbutton', {
      name: 'Temperature',
    })

    const ruleContainer = matchRuleSelect.closest('.grid')
    expect(ruleContainer).not.toBeNull()
    expect(ruleContainer).toBe(excludeWordsInput.closest('.grid'))

    expect(keywordsInput.closest('form > .grid')).toBeNull()

    const paramsContainer = maxTokensInput.closest('.grid')
    expect(paramsContainer).not.toBeNull()
    expect(paramsContainer).toBe(temperatureInput.closest('.grid'))
    expect(paramsContainer).not.toBe(ruleContainer)
  })
})

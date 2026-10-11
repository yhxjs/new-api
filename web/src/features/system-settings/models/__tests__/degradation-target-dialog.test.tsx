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
import { createInstance } from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  AddTargetDialog,
  type AddTargetDialogProps,
} from '../degradation-target-dialog'

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'Add Target Channel': 'Add Target Channel',
        'Target Channel': 'Target Channel',
        'Select a channel...': 'Select a channel...',
        'Monitored Models': 'Monitored Models',
        'Select all': 'Select all',
        'Clear All': 'Clear All',
        'Add Target': 'Add Target',
        Cancel: 'Cancel',
        'Already added': 'Already added',
        'Selected {{count}} of {{total}}': 'Selected {{count}} of {{total}}',
        'Please select a channel to view its models.':
          'Please select a channel to view its models.',
        'This channel has no models configured.':
          'This channel has no models configured.',
      },
    },
  },
})

const mockChannels = [
  {
    id: 10,
    name: 'Official Anthropic',
    group: 'default',
    models: 'claude-3-7-sonnet,claude-3-5-haiku',
    status: 1,
  },
  {
    id: 20,
    name: 'Official OpenAI',
    group: 'vip',
    models: 'gpt-4o,o3-mini',
    status: 1,
  },
  {
    id: 30,
    name: 'Empty Channel',
    group: 'default',
    models: '',
    status: 1,
  },
]

let queryClient: QueryClient

function renderDialog(props: Partial<AddTargetDialogProps> = {}) {
  const defaultProps: AddTargetDialogProps = {
    open: true,
    onOpenChange: vi.fn(),
    existingChannelIds: [20],
    onAddTarget: vi.fn(),
    ...props,
  }

  return {
    ...render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <AddTargetDialog {...defaultProps} />
        </I18nextProvider>
      </QueryClientProvider>
    ),
    props: defaultProps,
  }
}

describe('AddTargetDialog', () => {
  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    })
    vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
      if (typeof url === 'string' && url.includes('/api/channel/search')) {
        return {
          data: {
            success: true,
            data: {
              items: mockChannels,
              total: mockChannels.length,
            },
          },
        }
      }
      return { data: { success: true, data: [] } }
    })
  })

  afterEach(() => {
    cleanup()
    queryClient.clear()
    vi.restoreAllMocks()
  })

  test('renders dialog and shows placeholder before channel is chosen', async () => {
    renderDialog()

    expect(
      screen.getByRole('dialog', { name: 'Add Target Channel' })
    ).toBeInTheDocument()
    expect(await screen.findByText('Select a channel...')).toBeInTheDocument()
    expect(
      screen.getByText('Please select a channel to view its models.')
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add Target' })).toBeDisabled()
  })

  test('selects channel, parses models as initially unselected, and enables add upon checking a model', async () => {
    const user = userEvent.setup()
    const onAddTarget = vi.fn()
    const onOpenChange = vi.fn()
    renderDialog({ onAddTarget, onOpenChange })

    const dialog = within(
      screen.getByRole('dialog', { name: 'Add Target Channel' })
    )
    await user.click(dialog.getByRole('combobox'))

    const option = await screen.findByRole('option', {
      name: /Official Anthropic/i,
    })
    await user.click(option)

    // Combobox displays selected channel
    expect(dialog.getByRole('combobox')).toHaveTextContent('Official Anthropic')

    // Models should be displayed and initially unselected (per requirement)
    expect(screen.getByText(/Selected 0 of 2/)).toBeInTheDocument()
    const sonnetCheckbox = dialog.getByRole('checkbox', {
      name: 'claude-3-7-sonnet',
    })
    const haikuCheckbox = dialog.getByRole('checkbox', {
      name: 'claude-3-5-haiku',
    })
    expect(sonnetCheckbox).not.toBeChecked()
    expect(haikuCheckbox).not.toBeChecked()

    // Add button remains disabled until at least one model is selected
    const addButton = dialog.getByRole('button', { name: 'Add Target' })
    expect(addButton).toBeDisabled()

    // Select sonnet model
    await user.click(sonnetCheckbox)
    expect(sonnetCheckbox).toBeChecked()
    expect(screen.getByText(/Selected 1 of 2/)).toBeInTheDocument()
    expect(addButton).toBeEnabled()

    // Click Add Target
    await user.click(addButton)
    expect(onAddTarget).toHaveBeenCalledWith({
      channel_id: 10,
      channel_name: 'Official Anthropic',
      models: ['claude-3-7-sonnet'],
      bindings: [{ model_name: 'claude-3-7-sonnet', enabled: true }],
    })
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  test('Select all and Clear All toggle all models', async () => {
    const user = userEvent.setup()
    renderDialog()

    const dialog = within(
      screen.getByRole('dialog', { name: 'Add Target Channel' })
    )
    await user.click(dialog.getByRole('combobox'))
    await user.click(
      await screen.findByRole('option', { name: /Official Anthropic/i })
    )

    const selectAllBtn = dialog.getByRole('button', { name: 'Select all' })
    const clearAllBtn = dialog.getByRole('button', { name: 'Clear All' })

    await user.click(selectAllBtn)
    expect(screen.getByText(/Selected 2 of 2/)).toBeInTheDocument()
    expect(
      dialog.getByRole('checkbox', { name: 'claude-3-7-sonnet' })
    ).toBeChecked()
    expect(
      dialog.getByRole('checkbox', { name: 'claude-3-5-haiku' })
    ).toBeChecked()

    await user.click(clearAllBtn)
    expect(screen.getByText(/Selected 0 of 2/)).toBeInTheDocument()
    expect(
      dialog.getByRole('checkbox', { name: 'claude-3-7-sonnet' })
    ).not.toBeChecked()
    expect(
      dialog.getByRole('checkbox', { name: 'claude-3-5-haiku' })
    ).not.toBeChecked()
  })

  test('marks already monitored channels as disabled in option list', async () => {
    const user = userEvent.setup()
    renderDialog({ existingChannelIds: [20] })

    const dialog = within(
      screen.getByRole('dialog', { name: 'Add Target Channel' })
    )
    await user.click(dialog.getByRole('combobox'))

    const disabledOption = await screen.findByRole('option', {
      name: /Official OpenAI/i,
    })
    expect(disabledOption).toHaveAttribute('data-disabled', 'true')
    expect(disabledOption).toHaveTextContent('Already added')
  })

  test('shows empty state when selected channel has no models configured', async () => {
    const user = userEvent.setup()
    renderDialog()

    const dialog = within(
      screen.getByRole('dialog', { name: 'Add Target Channel' })
    )
    await user.click(dialog.getByRole('combobox'))
    await user.click(
      await screen.findByRole('option', { name: /Empty Channel/i })
    )

    expect(
      screen.getByText('This channel has no models configured.')
    ).toBeInTheDocument()
    expect(dialog.getByRole('button', { name: 'Add Target' })).toBeDisabled()
  })
})

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
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosHeaders, type AxiosAdapter } from 'axios'
import { useMemo, type ComponentProps } from 'react'
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import {
  getOptionValue,
  useSystemOptions,
} from '../../hooks/use-system-options'
import type { UpdateOptionRequest } from '../../types'
import {
  DegradationCheckSection,
  type ProbeConfig,
} from '../degradation-check-section'

type SettingsDefaults = ComponentProps<
  typeof DegradationCheckSection
>['defaultValues']

const defaults: SettingsDefaults = {
  'degradation_setting.enabled': false,
  'degradation_setting.interval_minutes': 10,
  'degradation_setting.concurrency': 1,
  'degradation_setting.channel_jitter_ms': 100,
  'degradation_setting.timeout_seconds': 30,
  'degradation_setting.retention_days': 7,
  'degradation_setting.public_mask_topology': true,
  'degradation_setting.targets': '[]',
  'degradation_setting.probes': '[]',
}

const mockChannels = [
  {
    id: 1,
    name: 'OpenAI Primary',
    group: 'default',
    models: 'o3,gemini-2.5-pro',
    status: 1,
  },
  {
    id: 2,
    name: 'OpenAI Secondary',
    group: 'default',
    models: 'gpt-4o-mini',
    status: 1,
  },
  {
    id: 3,
    name: 'OpenAI Backup',
    group: 'default',
    models: 'gpt-4o',
    status: 1,
  },
]
const regexProbe: ProbeConfig = {
  id: 'comma-probe',
  name: 'Comma response',
  prompt: 'Reply with a,b',
  keywords: ['^a,b$'],
  match_rule: 'regex',
  case_sensitive: false,
  max_tokens: 256,
}
let queryClient: QueryClient
let actionsContainer: HTMLDivElement

function renderSettings(defaultValues = defaults) {
  return render(
    <QueryClientProvider client={queryClient}>
      <SettingsPageProvider actionsContainer={actionsContainer}>
        <DegradationCheckSection defaultValues={defaultValues} />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
}

function QueriedSettings() {
  const { data, isLoading } = useSystemOptions()
  const defaultValues = useMemo(
    () => getOptionValue(data?.data, defaults),
    [data?.data]
  )
  if (isLoading) return null
  return <DegradationCheckSection defaultValues={defaultValues} />
}

function renderQueriedSettings() {
  return render(
    <QueryClientProvider client={queryClient}>
      <SettingsPageProvider actionsContainer={actionsContainer}>
        <QueriedSettings />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
}

function mockPersistedSettings(initialValues = defaults) {
  const serverValues: Record<string, string | boolean | number> = {
    ...initialValues,
  }
  const readOptions = async () => ({
    data: {
      success: true,
      message: '',
      data: Object.entries(serverValues).map(([key, value]) => ({
        key,
        value: String(value),
      })),
    },
  })
  const persistOption = async (_url: string, body?: unknown) => {
    const request = body as UpdateOptionRequest
    serverValues[request.key] = request.value
    return { data: { success: true, message: '' } }
  }
  const mockGet = async (url: string) => {
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
    return readOptions()
  }
  vi.spyOn(api, 'get').mockImplementation(mockGet)
  vi.mocked(api.put).mockImplementation(persistOption)
  return { serverValues, readOptions, persistOption, mockGet }
}

function savedOptions(): Record<string, string | boolean | number> {
  const requests = vi
    .mocked(api.put)
    .mock.calls.map((call) => call[1] as UpdateOptionRequest)
  return Object.fromEntries(
    requests.map((request) => [request.key, request.value])
  )
}

describe('degradation settings persistence', () => {
  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    actionsContainer = document.createElement('div')
    document.body.append(actionsContainer)
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
    vi.spyOn(api, 'put').mockResolvedValue({
      data: { success: true, message: '' },
      status: 200,
      statusText: 'OK',
      headers: {},
      config: { headers: new AxiosHeaders() },
    })
  })

  afterEach(() => {
    cleanup()
    queryClient.clear()
    actionsContainer.remove()
  })

  test('Save Changes submits a newly created regex probe and reload retains its expression', async () => {
    const user = userEvent.setup()
    const view = renderSettings()
    await user.click(screen.getByRole('button', { name: 'Add Custom Probe' }))
    const dialog = within(screen.getByRole('dialog', { name: 'Create Probe' }))
    await user.type(
      dialog.getByPlaceholderText('e.g. Basic Math Reasoning'),
      'Comma response'
    )
    await user.tab()
    await user.tab()
    await user.keyboard('Reply with a,b')
    await user.click(dialog.getByRole('combobox'))
    await user.click(screen.getByRole('option', { name: 'Regex' }))
    await user.type(
      dialog.getByRole('textbox', {
        name: 'Expected Regular Expressions (one per line)',
      }),
      '^a,b$'
    )
    await user.click(dialog.getByRole('button', { name: 'Save Probe' }))
    expect(api.put).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => {
      expect(savedOptions()).toEqual({
        'degradation_setting.probes': expect.any(String),
      })
      expect(
        JSON.parse(String(savedOptions()['degradation_setting.probes']))
      ).toEqual([{ ...regexProbe, id: expect.any(String) }])
    })
    const persistedProbes = String(savedOptions()['degradation_setting.probes'])
    view.unmount()
    renderSettings({
      ...defaults,
      'degradation_setting.probes': persistedProbes,
    })
    await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
    expect(
      within(screen.getByRole('dialog', { name: 'Edit Probe' })).getByRole(
        'textbox',
        {
          name: 'Expected Regular Expressions (one per line)',
        }
      )
    ).toHaveValue('^a,b$')
  })

  test('Save Changes persists modified dotted scalar settings instead of their prefilled values', async () => {
    const user = userEvent.setup()
    renderSettings()
    const fields = [
      ['Check Interval (Minutes)', 10, 15],
      ['Max Cross-Channel Concurrency', 1, 3],
      ['In-Channel Jitter Delay (ms)', 100, 150],
      ['Hard Timeout (Seconds)', 30, 45],
      ['History Retention Days', 7, 9],
    ] as const
    for (const [name, initialValue, value] of fields) {
      const input = screen.getByRole('spinbutton', { name })
      expect(input).toHaveValue(initialValue)
      fireEvent.change(input, { target: { value: String(value) } })
    }
    await user.click(
      screen.getByRole('switch', { name: 'Enable Scheduled Degradation Check' })
    )
    await user.click(screen.getByRole('switch', { name: '' }))
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => {
      expect(savedOptions()).toEqual({
        'degradation_setting.enabled': true,
        'degradation_setting.interval_minutes': 15,
        'degradation_setting.concurrency': 3,
        'degradation_setting.channel_jitter_ms': 150,
        'degradation_setting.timeout_seconds': 45,
        'degradation_setting.retention_days': 9,
        'degradation_setting.public_mask_topology': false,
      })
    })
    expect(
      screen.getByRole('spinbutton', { name: 'Check Interval (Minutes)' })
    ).toHaveValue(15)
  })

  test('an out-of-range dotted interval blocks settings API writes', async () => {
    const user = userEvent.setup()
    renderSettings()
    const intervalInput = screen.getByRole('spinbutton', {
      name: 'Check Interval (Minutes)',
    })
    fireEvent.change(intervalInput, { target: { value: '0' } })

    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => {
      expect(intervalInput).toHaveAttribute('aria-invalid', 'true')
    })
    expect(api.put).not.toHaveBeenCalled()
  })

  test('saving an interval draft after a settings refresh preserves unedited options changed by another administrator', async () => {
    const user = userEvent.setup()
    const { serverValues } = mockPersistedSettings()
    const successToast = vi.spyOn(toast, 'success')
    renderQueriedSettings()
    const interval = await screen.findByRole('spinbutton', {
      name: 'Check Interval (Minutes)',
    })
    fireEvent.change(interval, { target: { value: '15' } })
    const remoteProbes = JSON.stringify([regexProbe])
    const remoteTargets = JSON.stringify([
      { channel_id: 2, models: ['gpt-4o-mini'] },
    ])
    serverValues['degradation_setting.timeout_seconds'] = 45
    serverValues['degradation_setting.probes'] = remoteProbes
    serverValues['degradation_setting.targets'] = remoteTargets
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: ['system-options'] })
    })
    expect(interval).toHaveValue(15)

    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => {
      expect(successToast).toHaveBeenCalledWith('Settings updated successfully')
    })
    expect(serverValues['degradation_setting.interval_minutes']).toBe(15)
    expect(serverValues['degradation_setting.timeout_seconds']).toBe(45)
    expect(serverValues['degradation_setting.probes']).toBe(remoteProbes)
    expect(serverValues['degradation_setting.targets']).toBe(remoteTargets)
    expect(savedOptions()).toEqual({
      'degradation_setting.interval_minutes': 15,
    })
    const timeout = screen.getByRole('spinbutton', {
      name: 'Hard Timeout (Seconds)',
    })
    expect(timeout).toHaveValue(45)
    vi.mocked(api.put).mockClear()
    fireEvent.change(timeout, { target: { value: '30' } })
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => {
      expect(serverValues['degradation_setting.timeout_seconds']).toBe(30)
    })
    expect(savedOptions()).toEqual({
      'degradation_setting.timeout_seconds': 30,
    })
  })

  test('saving adopts refreshed unedited values while retaining a new field edited during the request', async () => {
    const user = userEvent.setup()
    const { serverValues, persistOption } = mockPersistedSettings()
    let releaseWrite: () => void = () => undefined
    const pendingWrite = new Promise<void>((resolve) => {
      releaseWrite = resolve
    })
    vi.mocked(api.put).mockImplementationOnce(async (url, body) => {
      const response = await persistOption(url, body)
      await pendingWrite
      return response
    })
    const successToast = vi.spyOn(toast, 'success')
    renderQueriedSettings()
    const interval = await screen.findByRole('spinbutton', {
      name: 'Check Interval (Minutes)',
    })
    fireEvent.change(interval, { target: { value: '15' } })
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
    serverValues['degradation_setting.timeout_seconds'] = 45
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: ['system-options'] })
    })
    const concurrency = screen.getByRole('spinbutton', {
      name: 'Max Cross-Channel Concurrency',
    })
    fireEvent.change(concurrency, { target: { value: '3' } })
    releaseWrite()
    await waitFor(() => {
      expect(successToast).toHaveBeenCalledWith('Settings updated successfully')
    })
    expect(
      screen.getByRole('spinbutton', { name: 'Hard Timeout (Seconds)' })
    ).toHaveValue(45)
    expect(concurrency).toHaveValue(3)
    expect(serverValues['degradation_setting.concurrency']).toBe(1)
    vi.mocked(api.put).mockClear()

    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() =>
      expect(serverValues['degradation_setting.concurrency']).toBe(3)
    )
    expect(savedOptions()).toEqual({ 'degradation_setting.concurrency': 3 })
  })

  test.each(['probes', 'targets'] as const)(
    'a new %s draft created while saving still detects a refreshed external conflict on the next save',
    async (field) => {
      const user = userEvent.setup()
      const { serverValues, persistOption } = mockPersistedSettings()
      let releaseWrite: () => void = () => undefined
      const pendingWrite = new Promise<void>((resolve) => {
        releaseWrite = resolve
      })
      vi.mocked(api.put).mockImplementationOnce(async (url, body) => {
        const response = await persistOption(url, body)
        await pendingWrite
        return response
      })
      const errorToast = vi.spyOn(toast, 'error')
      const successToast = vi.spyOn(toast, 'success')
      renderQueriedSettings()
      const interval = await screen.findByRole('spinbutton', {
        name: 'Check Interval (Minutes)',
      })
      fireEvent.change(interval, { target: { value: '15' } })
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))
      await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
      const optionKey = `degradation_setting.${field}`
      const remoteValue = JSON.stringify(
        field === 'probes'
          ? [regexProbe]
          : [{ channel_id: 2, models: ['gpt-4o-mini'] }]
      )
      serverValues[optionKey] = remoteValue
      await act(async () => {
        await queryClient.invalidateQueries({ queryKey: ['system-options'] })
      })
      if (field === 'probes') {
        await user.click(
          screen.getByRole('button', { name: 'Add Custom Probe' })
        )
        const dialog = within(
          screen.getByRole('dialog', { name: 'Create Probe' })
        )
        await user.type(
          dialog.getByPlaceholderText('e.g. Basic Math Reasoning'),
          'Local probe'
        )
        await user.tab()
        await user.tab()
        await user.keyboard('Reply with local')
        await user.type(
          dialog.getByRole('textbox', {
            name: 'Expected Keywords (comma-separated)',
          }),
          'local'
        )
        await user.click(dialog.getByRole('button', { name: 'Save Probe' }))
      } else {
        await user.click(
          screen.getByRole('button', { name: 'Add Target Channel' })
        )
        const dialog = within(
          screen.getByRole('dialog', { name: 'Add Target Channel' })
        )
        await user.click(dialog.getByRole('combobox'))
        await user.click(screen.getByRole('option', { name: /OpenAI Backup/i }))
        await user.click(dialog.getByRole('checkbox', { name: 'gpt-4o' }))
        await user.click(dialog.getByRole('button', { name: 'Add Target' }))
      }
      releaseWrite()
      await waitFor(() => {
        expect(successToast).toHaveBeenCalledWith(
          'Settings updated successfully'
        )
      })
      vi.mocked(api.put).mockClear()

      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(errorToast).toHaveBeenCalledWith(
          'Probe or target configuration changed elsewhere. Reset to reload before saving.'
        )
      })
      expect(api.put).not.toHaveBeenCalled()
      expect(serverValues[optionKey]).toBe(remoteValue)
      if (field === 'probes') {
        expect(screen.getByText('Local probe')).toBeInTheDocument()
      } else {
        expect(
          screen.getByRole('group', { name: 'gpt-4o' })
        ).toBeInTheDocument()
      }
    }
  )

  test.each(['probes', 'targets'] as const)(
    'a refreshed conflict in edited %s blocks writes until Reset reloads the latest configuration',
    async (field) => {
      const user = userEvent.setup()
      const target = {
        channel_id: 1,
        models: ['gpt-4o'],
        bindings: [{ model_name: 'gpt-4o', enabled: true }],
      }
      const remoteProbe = {
        ...regexProbe,
        id: 'remote-probe',
        name: 'Remote probe',
      }
      const remoteTarget = { channel_id: 2, models: ['gpt-4o-mini'] }
      const { serverValues } = mockPersistedSettings({
        ...defaults,
        'degradation_setting.probes': JSON.stringify([regexProbe]),
        'degradation_setting.targets': JSON.stringify([target]),
      })
      const errorToast = vi.spyOn(toast, 'error')
      const successToast = vi.spyOn(toast, 'success')
      renderQueriedSettings()
      const interval = await screen.findByRole('spinbutton', {
        name: 'Check Interval (Minutes)',
      })
      fireEvent.change(interval, { target: { value: '15' } })
      if (field === 'probes') {
        await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
        const dialog = within(
          screen.getByRole('dialog', { name: 'Edit Probe' })
        )
        fireEvent.change(
          dialog.getByRole('textbox', {
            name: 'Expected Regular Expressions (one per line)',
          }),
          { target: { value: '^draft$' } }
        )
        await user.click(dialog.getByRole('button', { name: 'Save Probe' }))
      } else {
        await user.click(screen.getByRole('switch', { name: 'gpt-4o' }))
      }
      const optionKey = `degradation_setting.${field}`
      const remoteValue = JSON.stringify(
        field === 'probes' ? [regexProbe, remoteProbe] : [target, remoteTarget]
      )
      serverValues[optionKey] = remoteValue
      await act(async () => {
        await queryClient.invalidateQueries({ queryKey: ['system-options'] })
      })

      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(errorToast).toHaveBeenCalledWith(
          'Probe or target configuration changed elsewhere. Reset to reload before saving.'
        )
      })
      expect(api.put).not.toHaveBeenCalled()
      expect(serverValues[optionKey]).toBe(remoteValue)
      expect(interval).toHaveValue(15)
      await user.click(screen.getByRole('button', { name: 'Reset' }))
      expect(interval).toHaveValue(10)
      if (field === 'probes') {
        expect(screen.getByText('Remote probe')).toBeInTheDocument()
        await user.click(
          screen.getAllByRole('button', { name: 'Edit Probe' })[0]
        )
        const dialog = within(
          screen.getByRole('dialog', { name: 'Edit Probe' })
        )
        fireEvent.change(
          dialog.getByRole('textbox', {
            name: 'Expected Regular Expressions (one per line)',
          }),
          { target: { value: '^after-reset$' } }
        )
        await user.click(dialog.getByRole('button', { name: 'Save Probe' }))
      } else {
        expect(
          screen.getByRole('group', { name: 'gpt-4o-mini' })
        ).toBeInTheDocument()
        await user.click(screen.getByRole('switch', { name: 'gpt-4o' }))
      }
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))
      await waitFor(() => {
        expect(successToast).toHaveBeenCalledWith(
          'Settings updated successfully'
        )
      })
      expect(JSON.parse(String(serverValues[optionKey]))).toEqual(
        field === 'probes'
          ? [{ ...regexProbe, keywords: ['^after-reset$'] }, remoteProbe]
          : [
              {
                ...target,
                bindings: [{ model_name: 'gpt-4o', enabled: false }],
              },
              remoteTarget,
            ]
      )
      expect(savedOptions()).toEqual({ [optionKey]: serverValues[optionKey] })
    }
  )

  test.each(['probes', 'targets'] as const)(
    'a refreshed %s value matching the draft can still be saved',
    async (field) => {
      const user = userEvent.setup()
      const target = {
        channel_id: 1,
        models: ['gpt-4o'],
        bindings: [{ model_name: 'gpt-4o', enabled: true }],
      }
      const { serverValues } = mockPersistedSettings({
        ...defaults,
        'degradation_setting.probes': JSON.stringify([regexProbe]),
        'degradation_setting.targets': JSON.stringify([target]),
      })
      const successToast = vi.spyOn(toast, 'success')
      renderQueriedSettings()
      await screen.findByRole('button', { name: 'Edit Probe' })
      if (field === 'probes') {
        await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
        const dialog = within(
          screen.getByRole('dialog', { name: 'Edit Probe' })
        )
        fireEvent.change(
          dialog.getByRole('textbox', {
            name: 'Expected Regular Expressions (one per line)',
          }),
          { target: { value: '^draft$' } }
        )
        await user.click(dialog.getByRole('button', { name: 'Save Probe' }))
      } else {
        await user.click(screen.getByRole('switch', { name: 'gpt-4o' }))
      }
      const optionKey = `degradation_setting.${field}`
      const draft = JSON.stringify(
        field === 'probes'
          ? [{ ...regexProbe, keywords: ['^draft$'] }]
          : [
              {
                ...target,
                bindings: [{ model_name: 'gpt-4o', enabled: false }],
              },
            ]
      )
      serverValues[optionKey] = draft
      await act(async () => {
        await queryClient.invalidateQueries({ queryKey: ['system-options'] })
      })

      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      await waitFor(() => {
        expect(successToast).toHaveBeenCalledWith(
          'Settings updated successfully'
        )
      })
      expect(savedOptions()).toEqual({ [optionKey]: draft })
    }
  )

  test('retrying after a partial JSON save preserves new edits without treating its own write as a conflict', async () => {
    const user = userEvent.setup()
    const target = {
      channel_id: 1,
      models: ['gpt-4o'],
      bindings: [{ model_name: 'gpt-4o', enabled: true }],
    }
    const { serverValues, persistOption } = mockPersistedSettings({
      ...defaults,
      'degradation_setting.probes': JSON.stringify([regexProbe]),
      'degradation_setting.targets': JSON.stringify([target]),
    })
    let releaseTargetWrite: () => void = () => undefined
    const pendingTargetWrite = new Promise<void>((resolve) => {
      releaseTargetWrite = resolve
    })
    vi.mocked(api.put)
      .mockImplementationOnce(async (url, body) => {
        await pendingTargetWrite
        return persistOption(url, body)
      })
      .mockResolvedValueOnce({
        data: { success: false, message: 'probes failed' },
      })
    const errorToast = vi.spyOn(toast, 'error')
    const successToast = vi.spyOn(toast, 'success')
    renderQueriedSettings()
    await screen.findByRole('button', { name: 'Edit Probe' })
    await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
    const dialog = within(screen.getByRole('dialog', { name: 'Edit Probe' }))
    fireEvent.change(
      dialog.getByRole('textbox', {
        name: 'Expected Regular Expressions (one per line)',
      }),
      { target: { value: '^draft$' } }
    )
    await user.click(dialog.getByRole('button', { name: 'Save Probe' }))
    const modelSwitch = screen.getByRole('switch', { name: 'gpt-4o' })
    await user.click(modelSwitch)
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
    await user.click(modelSwitch)
    releaseTargetWrite()
    await waitFor(() => {
      expect(errorToast).toHaveBeenCalledWith(
        'probes failed — Some options may have already been applied'
      )
      expect(queryClient.getQueryState(['system-options'])?.fetchStatus).toBe(
        'idle'
      )
    })
    expect(
      JSON.parse(String(serverValues['degradation_setting.targets']))
    ).toEqual([
      { ...target, bindings: [{ model_name: 'gpt-4o', enabled: false }] },
    ])
    expect(modelSwitch).toBeChecked()
    vi.mocked(api.put).mockClear()

    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => {
      expect(successToast).toHaveBeenCalledWith('Settings updated successfully')
    })
    expect(
      JSON.parse(String(serverValues['degradation_setting.targets']))
    ).toEqual([target])
    expect(
      JSON.parse(String(serverValues['degradation_setting.probes']))
    ).toEqual([{ ...regexProbe, keywords: ['^draft$'] }])
  })

  test('retrying a partially saved JSON draft refreshes a pending earlier read before checking conflicts', async () => {
    const user = userEvent.setup()
    const target = {
      channel_id: 1,
      models: ['gpt-4o'],
      bindings: [{ model_name: 'gpt-4o', enabled: true }],
    }
    const { serverValues, readOptions, persistOption } = mockPersistedSettings({
      ...defaults,
      'degradation_setting.probes': JSON.stringify([regexProbe]),
      'degradation_setting.targets': JSON.stringify([target]),
    })
    type ReadResponse = Awaited<ReturnType<typeof readOptions>>
    let releasePartialRead: (response: ReadResponse) => void = () => undefined
    const pendingPartialRead = new Promise<ReadResponse>((resolve) => {
      releasePartialRead = resolve
    })
    let partialReadStarted = false
    vi.mocked(api.get).mockImplementation(async () => {
      if (vi.mocked(api.put).mock.calls.length > 0 && !partialReadStarted) {
        partialReadStarted = true
        return pendingPartialRead
      }
      return readOptions()
    })
    let releaseTargetWrite: () => void = () => undefined
    const pendingTargetWrite = new Promise<void>((resolve) => {
      releaseTargetWrite = resolve
    })
    vi.mocked(api.put)
      .mockImplementationOnce(async (url, body) => {
        await pendingTargetWrite
        return persistOption(url, body)
      })
      .mockResolvedValueOnce({
        data: { success: false, message: 'probes failed' },
      })
    const errorToast = vi.spyOn(toast, 'error')
    const successToast = vi.spyOn(toast, 'success')
    try {
      renderQueriedSettings()
      await screen.findByRole('button', { name: 'Edit Probe' })
      await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
      const probeDialog = within(
        screen.getByRole('dialog', { name: 'Edit Probe' })
      )
      fireEvent.change(
        probeDialog.getByRole('textbox', {
          name: 'Expected Regular Expressions (one per line)',
        }),
        { target: { value: '^draft$' } }
      )
      await user.click(probeDialog.getByRole('button', { name: 'Save Probe' }))
      await user.click(screen.getByRole('switch', { name: 'gpt-4o' }))
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))
      await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
      await user.click(
        screen.getByRole('button', { name: 'Configure Probes & Reasoning' })
      )
      const bindingDialog = within(
        screen.getByRole('dialog', { name: 'Configure Model Probes: gpt-4o' })
      )
      await user.click(bindingDialog.getByRole('combobox'))
      await user.click(screen.getByRole('option', { name: 'High' }))
      await user.click(bindingDialog.getByRole('button', { name: 'Save' }))
      releaseTargetWrite()
      await waitFor(() => {
        expect(errorToast).toHaveBeenCalledWith(
          'probes failed — Some options may have already been applied'
        )
        expect(
          screen.getByRole('button', { name: 'Save Changes' })
        ).toBeEnabled()
      })
      expect(partialReadStarted).toBe(true)

      await user.click(screen.getByRole('button', { name: 'Save Changes' }))

      expect(errorToast).not.toHaveBeenCalledWith(
        'Probe or target configuration changed elsewhere. Reset to reload before saving.'
      )
      await waitFor(() => {
        expect(successToast).toHaveBeenCalledWith(
          'Settings updated successfully'
        )
      })
      expect(
        JSON.parse(String(serverValues['degradation_setting.targets']))
      ).toEqual([
        {
          ...target,
          bindings: [
            { model_name: 'gpt-4o', enabled: false, reasoning_effort: 'high' },
          ],
        },
      ])
    } finally {
      await act(async () => {
        releaseTargetWrite()
        releasePartialRead(await readOptions())
      })
    }
  })

  test('an edited JSON draft retries a failed settings read and blocks writes until the latest configuration loads', async () => {
    const user = userEvent.setup()
    const { serverValues, mockGet } = mockPersistedSettings({
      ...defaults,
      'degradation_setting.probes': JSON.stringify([regexProbe]),
    })
    const errorToast = vi.spyOn(toast, 'error')
    const successToast = vi.spyOn(toast, 'success')
    renderQueriedSettings()
    await screen.findByRole('button', { name: 'Edit Probe' })
    await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
    const dialog = within(screen.getByRole('dialog', { name: 'Edit Probe' }))
    fireEvent.change(
      dialog.getByRole('textbox', {
        name: 'Expected Regular Expressions (one per line)',
      }),
      { target: { value: '^draft$' } }
    )
    await user.click(dialog.getByRole('button', { name: 'Save Probe' }))
    vi.mocked(api.get).mockRejectedValue(new Error('options unavailable'))
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: ['system-options'] })
    })

    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() =>
      expect(errorToast).toHaveBeenCalledWith('Failed to load')
    )
    expect(api.put).not.toHaveBeenCalled()
    expect(
      JSON.parse(String(serverValues['degradation_setting.probes']))
    ).toEqual([regexProbe])
    vi.mocked(api.get).mockImplementation(mockGet)
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => {
      expect(successToast).toHaveBeenCalledWith('Settings updated successfully')
    })
    expect(
      JSON.parse(String(serverValues['degradation_setting.probes']))
    ).toEqual([{ ...regexProbe, keywords: ['^draft$'] }])
  })

  test('a partial save followed by a settings refetch retains scalar drafts for retry', async () => {
    const user = userEvent.setup()
    const { serverValues, persistOption } = mockPersistedSettings()
    vi.mocked(api.put)
      .mockImplementationOnce(persistOption)
      .mockResolvedValueOnce({
        data: { success: false, message: 'interval failed' },
      })
    const errorToast = vi.spyOn(toast, 'error')
    const successToast = vi.spyOn(toast, 'success')
    renderQueriedSettings()
    const interval = await screen.findByRole('spinbutton', {
      name: 'Check Interval (Minutes)',
    })
    fireEvent.change(interval, { target: { value: '15' } })
    fireEvent.change(
      screen.getByRole('spinbutton', { name: 'Max Cross-Channel Concurrency' }),
      { target: { value: '3' } }
    )
    await user.click(
      screen.getByRole('switch', { name: 'Enable Scheduled Degradation Check' })
    )
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => {
      expect(errorToast).toHaveBeenCalledWith(
        'interval failed — Some options may have already been applied'
      )
      expect(queryClient.getQueryState(['system-options'])?.fetchStatus).toBe(
        'idle'
      )
    })
    expect(serverValues['degradation_setting.enabled']).toBe(true)
    expect(serverValues['degradation_setting.interval_minutes']).toBe(10)
    expect(serverValues['degradation_setting.concurrency']).toBe(1)
    expect(interval).toHaveValue(15)
    expect(
      screen.getByRole('spinbutton', { name: 'Max Cross-Channel Concurrency' })
    ).toHaveValue(3)
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => {
      expect(successToast).toHaveBeenCalledWith('Settings updated successfully')
      expect(queryClient.getQueryState(['system-options'])?.fetchStatus).toBe(
        'idle'
      )
    })
    expect(serverValues['degradation_setting.interval_minutes']).toBe(15)
    expect(serverValues['degradation_setting.concurrency']).toBe(3)
    expect(interval).toHaveValue(15)
  })

  test('a partial save followed by a settings refetch retains probe, target and binding drafts', async () => {
    const user = userEvent.setup()
    const target = {
      channel_id: 1,
      models: ['gpt-4o'],
      bindings: [{ model_name: 'gpt-4o', enabled: true }],
    }
    const { serverValues, persistOption } = mockPersistedSettings({
      ...defaults,
      'degradation_setting.probes': JSON.stringify([regexProbe]),
      'degradation_setting.targets': JSON.stringify([target]),
    })
    vi.mocked(api.put)
      .mockImplementationOnce(persistOption)
      .mockResolvedValueOnce({
        data: { success: false, message: 'interval failed' },
      })
    const errorToast = vi.spyOn(toast, 'error')
    const successToast = vi.spyOn(toast, 'success')
    renderQueriedSettings()
    await screen.findByRole('button', { name: 'Edit Probe' })
    await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
    const probeDialog = within(
      screen.getByRole('dialog', { name: 'Edit Probe' })
    )
    fireEvent.change(
      probeDialog.getByRole('textbox', {
        name: 'Expected Regular Expressions (one per line)',
      }),
      { target: { value: '^updated$' } }
    )
    await user.click(probeDialog.getByRole('button', { name: 'Save Probe' }))
    await user.click(
      screen.getByRole('button', { name: 'Configure Probes & Reasoning' })
    )
    const bindingDialog = within(
      screen.getByRole('dialog', { name: 'Configure Model Probes: gpt-4o' })
    )
    await user.click(bindingDialog.getByRole('combobox'))
    await user.click(screen.getByRole('option', { name: 'High' }))
    await user.click(bindingDialog.getByRole('button', { name: 'Save' }))
    await user.click(screen.getByRole('button', { name: 'Add Target Channel' }))
    const targetDialog = within(
      screen.getByRole('dialog', { name: 'Add Target Channel' })
    )
    await user.click(targetDialog.getByRole('combobox'))
    await user.click(screen.getByRole('option', { name: /OpenAI Secondary/i }))
    await user.click(
      targetDialog.getByRole('checkbox', { name: 'gpt-4o-mini' })
    )
    await user.click(targetDialog.getByRole('button', { name: 'Add Target' }))
    await user.click(
      screen.getByRole('switch', { name: 'Enable Scheduled Degradation Check' })
    )
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => {
      expect(errorToast).toHaveBeenCalledWith(
        'interval failed — Some options may have already been applied'
      )
      expect(queryClient.getQueryState(['system-options'])?.fetchStatus).toBe(
        'idle'
      )
    })
    expect(
      screen.getByRole('group', { name: 'gpt-4o-mini' })
    ).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Edit Probe' }))
    expect(
      within(screen.getByRole('dialog', { name: 'Edit Probe' })).getByRole(
        'textbox',
        { name: 'Expected Regular Expressions (one per line)' }
      )
    ).toHaveValue('^updated$')
    await user.click(screen.getByRole('button', { name: 'Save Probe' }))
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => {
      expect(successToast).toHaveBeenCalledWith('Settings updated successfully')
    })
    expect(
      JSON.parse(String(serverValues['degradation_setting.probes']))
    ).toEqual([{ ...regexProbe, keywords: ['^updated$'] }])
    expect(
      JSON.parse(String(serverValues['degradation_setting.targets']))
    ).toEqual([
      {
        ...target,
        bindings: [
          { model_name: 'gpt-4o', enabled: true, reasoning_effort: 'high' },
        ],
      },
      {
        channel_id: 2,
        channel_name: 'OpenAI Secondary',
        models: ['gpt-4o-mini'],
        bindings: [{ model_name: 'gpt-4o-mini', enabled: true }],
      },
    ])
  })

  test('successful saving preserves edits made while a request is pending and rebases the clean value', async () => {
    const user = userEvent.setup()
    const { serverValues, persistOption } = mockPersistedSettings()
    let resolvePendingOption: (value: {
      data: { success: boolean; message: string }
    }) => void = () => undefined
    const pendingOption = new Promise<{
      data: { success: boolean; message: string }
    }>((resolve) => {
      resolvePendingOption = resolve
    })
    vi.mocked(api.put)
      .mockImplementationOnce(persistOption)
      .mockImplementationOnce(async (_url, body) => {
        const request = body as UpdateOptionRequest
        serverValues[request.key] = request.value
        return pendingOption
      })
    const successToast = vi.spyOn(toast, 'success')
    renderQueriedSettings()
    const interval = await screen.findByRole('spinbutton', {
      name: 'Check Interval (Minutes)',
    })
    fireEvent.change(interval, { target: { value: '15' } })
    await user.click(
      screen.getByRole('switch', { name: 'Enable Scheduled Degradation Check' })
    )
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => {
      expect(api.put).toHaveBeenCalledTimes(2)
      expect(queryClient.getQueryState(['system-options'])?.fetchStatus).toBe(
        'idle'
      )
    })
    fireEvent.change(interval, { target: { value: '20' } })
    resolvePendingOption({ data: { success: true, message: '' } })
    await waitFor(() => {
      expect(successToast).toHaveBeenCalledWith('Settings updated successfully')
      expect(queryClient.getQueryState(['system-options'])?.fetchStatus).toBe(
        'idle'
      )
    })
    expect(serverValues['degradation_setting.interval_minutes']).toBe(15)
    expect(interval).toHaveValue(20)

    fireEvent.change(interval, { target: { value: '15' } })
    serverValues['degradation_setting.interval_minutes'] = 25
    await queryClient.invalidateQueries({ queryKey: ['system-options'] })
    await waitFor(() => expect(interval).toHaveValue(25))
  })

  test('the final settings refetch keeps Saving disabled and preserves edits until it completes', async () => {
    const user = userEvent.setup()
    const { serverValues, readOptions } = mockPersistedSettings()
    type ReadResponse = Awaited<ReturnType<typeof readOptions>>
    let resolvePartialRead: (response: ReadResponse) => void = () => undefined
    const partialRead = new Promise<ReadResponse>((resolve) => {
      resolvePartialRead = resolve
    })
    let partialReadStarted = false
    let resolvePendingRead: (response: ReadResponse) => void = () => undefined
    const pendingRead = new Promise<ReadResponse>((resolve) => {
      resolvePendingRead = resolve
    })
    vi.mocked(api.get).mockImplementation(async () => {
      if (!partialReadStarted && vi.mocked(api.put).mock.calls.length > 0) {
        partialReadStarted = true
        return partialRead
      }
      if (vi.mocked(api.put).mock.calls.length === 1) {
        return pendingRead
      }
      return readOptions()
    })
    const successToast = vi.spyOn(toast, 'success')
    renderQueriedSettings()
    const interval = await screen.findByRole('spinbutton', {
      name: 'Check Interval (Minutes)',
    })
    fireEvent.change(interval, { target: { value: '15' } })
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => {
      expect(api.put).toHaveBeenCalledTimes(1)
      expect(queryClient.getQueryState(['system-options'])?.fetchStatus).toBe(
        'fetching'
      )
    })
    expect(screen.getByRole('button', { name: 'Saving...' })).toBeDisabled()
    expect(successToast).not.toHaveBeenCalledWith(
      'Settings updated successfully'
    )
    fireEvent.change(interval, { target: { value: '20' } })
    resolvePendingRead({
      data: {
        success: true,
        message: '',
        data: Object.entries(serverValues).map(([key, value]) => ({
          key,
          value: String(value),
        })),
      },
    })
    await waitFor(() => {
      expect(successToast).toHaveBeenCalledWith('Settings updated successfully')
      expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled()
    })
    expect(interval).toHaveValue(20)
    expect(serverValues['degradation_setting.interval_minutes']).toBe(15)

    fireEvent.change(interval, { target: { value: '15' } })
    await act(async () => {
      resolvePartialRead({
        data: {
          success: true,
          message: '',
          data: Object.entries(defaults).map(([key, value]) => ({
            key,
            value: String(value),
          })),
        },
      })
    })
    expect(interval).toHaveValue(15)
  })

  test('real Axios transport performs a fresh post-save read while an earlier partial GET is pending', async () => {
    const user = userEvent.setup()
    const serverValues: Record<string, string | boolean | number> = {
      ...defaults,
    }
    const requests: UpdateOptionRequest[] = []
    let partialReadStarted = false
    let completeWritesRead = false
    let releasePartialRead: () => void = () => undefined
    let releaseIntervalWrite: () => void = () => undefined
    const pendingIntervalWrite = new Promise<void>((resolve) => {
      releaseIntervalWrite = resolve
    })
    const originalAdapter = api.defaults.adapter
    const adapter: AxiosAdapter = async (config) => {
      if (config.method === 'put') {
        const request = JSON.parse(config.data as string) as UpdateOptionRequest
        requests.push(request)
        if (requests.length === 2) await pendingIntervalWrite
        serverValues[request.key] = request.value
        return {
          config,
          data: { success: true, message: '' },
          headers: new AxiosHeaders(),
          status: 200,
          statusText: 'OK',
        }
      }
      const response = {
        config,
        data: {
          success: true,
          message: '',
          data: Object.entries(serverValues).map(([key, value]) => ({
            key,
            value: String(value),
          })),
        },
        headers: new AxiosHeaders(),
        status: 200,
        statusText: 'OK',
      }
      if (requests.length > 0 && !partialReadStarted) {
        partialReadStarted = true
        return new Promise<typeof response>((resolve) => {
          releasePartialRead = () => resolve(response)
        })
      }
      if (requests.length === 2) {
        completeWritesRead = true
      }
      return response
    }
    // Keep the production api.get wrapper, interceptors and serialization;
    // only the network transport is controlled by the fixture.
    vi.mocked(api.put).mockRestore()
    vi.mocked(api.get).mockRestore()
    api.defaults.adapter = adapter
    const successToast = vi.spyOn(toast, 'success')
    try {
      renderQueriedSettings()
      const interval = await screen.findByRole('spinbutton', {
        name: 'Check Interval (Minutes)',
      })
      fireEvent.change(interval, { target: { value: '15' } })
      await user.click(
        screen.getByRole('switch', {
          name: 'Enable Scheduled Degradation Check',
        })
      )
      await user.click(screen.getByRole('button', { name: 'Save Changes' }))
      await waitFor(() => {
        expect(requests).toHaveLength(2)
        expect(partialReadStarted).toBe(true)
      })
      fireEvent.change(interval, { target: { value: '20' } })
      releaseIntervalWrite()
      await waitFor(() => {
        expect(successToast).toHaveBeenCalledWith(
          'Settings updated successfully'
        )
        expect(
          screen.getByRole('button', { name: 'Save Changes' })
        ).toBeEnabled()
      })
      expect(completeWritesRead).toBe(true)
      expect(serverValues['degradation_setting.interval_minutes']).toBe(15)
      expect(interval).toHaveValue(20)

      fireEvent.change(interval, { target: { value: '15' } })
      await act(async () => releasePartialRead())
      expect(interval).toHaveValue(15)
      serverValues['degradation_setting.interval_minutes'] = 25
      await queryClient.invalidateQueries({ queryKey: ['system-options'] })
      await waitFor(() => expect(interval).toHaveValue(25))
    } finally {
      await act(async () => {
        releaseIntervalWrite()
        releasePartialRead()
      })
      api.defaults.adapter = originalAdapter
    }
  })

  test('adding named reasoning models inherits model settings in the saved target', async () => {
    const user = userEvent.setup()
    renderSettings()
    await user.click(screen.getByRole('button', { name: 'Add Target Channel' }))
    const dialog = within(
      screen.getByRole('dialog', { name: 'Add Target Channel' })
    )
    await user.click(dialog.getByRole('combobox'))
    await user.click(screen.getByRole('option', { name: /OpenAI Primary/i }))
    await user.click(dialog.getByRole('button', { name: 'Select all' }))
    await user.click(dialog.getByRole('button', { name: 'Add Target' }))
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => {
      expect(
        JSON.parse(String(savedOptions()['degradation_setting.targets']))
      ).toEqual([
        {
          channel_id: 1,
          channel_name: 'OpenAI Primary',
          models: ['o3', 'gemini-2.5-pro'],
          bindings: [
            { model_name: 'o3', enabled: true },
            { model_name: 'gemini-2.5-pro', enabled: true },
          ],
        },
      ])
    })
  })

  test('Save Changes persists changed model bindings while preserving prefilled probes and target models', async () => {
    const user = userEvent.setup()
    const literalProbe = {
      ...regexProbe,
      id: 'second-probe',
      name: 'Second probe',
    }
    const probesValue = JSON.stringify([regexProbe, literalProbe])
    const target = {
      channel_id: 1,
      channel_name: 'Main channel',
      models: ['gpt-4o'],
      bindings: [
        {
          model_name: 'gpt-4o',
          enabled: true,
          probe_ids: ['comma-probe', 'second-probe'],
          reasoning_effort: 'off',
        },
      ],
    }
    renderSettings({
      ...defaults,
      'degradation_setting.probes': probesValue,
      'degradation_setting.targets': JSON.stringify([target]),
    })
    await user.click(
      screen.getByRole('button', { name: 'Configure Probes & Reasoning' })
    )
    const dialog = within(
      screen.getByRole('dialog', { name: 'Configure Model Probes: gpt-4o' })
    )
    expect(dialog.getByRole('checkbox', { name: 'Second probe' })).toBeChecked()
    await user.click(dialog.getByRole('checkbox', { name: 'Second probe' }))
    await user.click(dialog.getByRole('combobox'))
    await user.click(screen.getByRole('option', { name: 'High' }))
    await user.click(dialog.getByRole('button', { name: 'Save' }))
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => {
      expect(savedOptions()).not.toHaveProperty('degradation_setting.probes')
      expect(
        JSON.parse(String(savedOptions()['degradation_setting.targets']))
      ).toEqual([
        {
          ...target,
          bindings: [
            {
              model_name: 'gpt-4o',
              enabled: true,
              probe_ids: ['comma-probe'],
              reasoning_effort: 'high',
            },
          ],
        },
      ])
    })
  })

  test('all-model targets expose live channel models and preserve the dynamic selection when a binding is saved', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { id: 1, models: 'gpt-4o,gpt-4o-mini' } },
    })
    vi.spyOn(api, 'post').mockResolvedValue({ data: { success: true } })
    renderSettings({
      ...defaults,
      'degradation_setting.probes': JSON.stringify([regexProbe]),
      'degradation_setting.targets': JSON.stringify([
        { channel_id: 1, models: [] },
      ]),
    })

    expect(screen.getByText('All Models')).toBeInTheDocument()
    expect(screen.getByText('Loading current models...')).toBeInTheDocument()
    await screen.findByText('gpt-4o-mini')
    expect(api.get).toHaveBeenCalledWith('/api/channel/1')
    const row = screen.getByRole('group', { name: 'gpt-4o-mini' })
    await user.click(within(row).getByRole('switch', { name: 'gpt-4o-mini' }))
    expect(
      within(row).getByRole('switch', { name: 'gpt-4o-mini' })
    ).not.toBeChecked()
    expect(within(row).getByText('Paused')).toBeInTheDocument()
    await user.click(within(row).getByRole('button', { name: 'Test Now' }))
    expect(api.post).toHaveBeenCalledWith('/api/degradation/run-target', {
      channel_id: 1,
      model_name: 'gpt-4o-mini',
    })
    await user.click(
      within(row).getByRole('button', { name: 'Configure Probes & Reasoning' })
    )
    const dialog = within(
      screen.getByRole('dialog', {
        name: 'Configure Model Probes: gpt-4o-mini',
      })
    )
    await user.click(dialog.getByRole('button', { name: 'Save' }))
    await user.click(screen.getByRole('button', { name: 'Save Changes' }))
    await waitFor(() => {
      expect(
        JSON.parse(String(savedOptions()['degradation_setting.targets']))
      ).toEqual([
        {
          channel_id: 1,
          models: [],
          bindings: [
            {
              model_name: 'gpt-4o-mini',
              enabled: false,
            },
          ],
        },
      ])
    })
  })

  test('all-model targets show a failed channel response and allow retrying the model load', async () => {
    const user = userEvent.setup()
    vi.spyOn(api, 'get')
      .mockResolvedValueOnce({
        data: { success: false, message: 'channel unavailable' },
      })
      .mockResolvedValueOnce({
        data: { success: true, data: { id: 1, models: 'gpt-4o-mini' } },
      })
    renderSettings({
      ...defaults,
      'degradation_setting.targets': JSON.stringify([{ channel_id: 1 }]),
    })

    const error = await screen.findByRole('alert')
    expect(within(error).getByText('Failed to load')).toBeInTheDocument()
    expect(screen.queryByText('No models available')).not.toBeInTheDocument()
    await user.click(within(error).getByRole('button', { name: 'Retry' }))

    await screen.findByRole('group', { name: 'gpt-4o-mini' })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  test('all-model targets with no channel models show an empty state', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { id: 1, models: '' } },
    })
    renderSettings({
      ...defaults,
      'degradation_setting.targets': JSON.stringify([
        { channel_id: 1, models: [] },
      ]),
    })

    expect(await screen.findByText('No models available')).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Test Now' })
    ).not.toBeInTheDocument()
  })
})

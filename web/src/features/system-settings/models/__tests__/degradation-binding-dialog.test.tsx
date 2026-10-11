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
import { toast } from 'sonner'
import { expect, test, vi } from 'vitest'

import { ModelBindingDialog } from '../degradation-binding-dialog'
import type {
  ModelProbeBinding,
  ProbeConfig,
} from '../degradation-check-section'

const probe: ProbeConfig = {
  id: 'p1',
  name: 'Math probe',
  prompt: '1+1?',
  keywords: ['2'],
  match_rule: 'any',
  case_sensitive: false,
  max_tokens: 256,
}

test.each<ModelProbeBinding | null>([
  null,
  { model_name: 'o3', enabled: true, reasoning_effort: '' },
])(
  'saving an inherited binding preserves the model default: %j',
  async (binding) => {
    const user = userEvent.setup()
    const onSave = vi.fn()
    render(
      <ModelBindingDialog
        open
        onOpenChange={() => undefined}
        modelName='o3'
        initialBinding={binding}
        probes={[probe]}
        onSave={onSave}
      />
    )

    expect(screen.getByRole('combobox')).toHaveTextContent('Use model default')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave).toHaveBeenCalledWith({
      model_name: 'o3',
      enabled: true,
      probe_ids: undefined,
      reasoning_effort: undefined,
      thinking_budget: undefined,
    })
  }
)

test('inherited probes become an explicit subset only after unchecking one', async () => {
  const user = userEvent.setup()
  const onSave = vi.fn()
  render(
    <ModelBindingDialog
      open
      onOpenChange={() => undefined}
      modelName='o3'
      initialBinding={null}
      probes={[probe, { ...probe, id: 'p2', name: 'Second probe' }]}
      onSave={onSave}
    />
  )

  const checkboxes = screen.getAllByRole('checkbox')
  expect(checkboxes[0]).toBeChecked()
  await user.click(checkboxes[0])
  await user.click(screen.getByRole('button', { name: 'Save' }))

  expect(onSave).toHaveBeenCalledWith({
    model_name: 'o3',
    enabled: true,
    probe_ids: ['p2'],
    reasoning_effort: undefined,
    thinking_budget: undefined,
  })
})

test('an explicit off binding remains off until the model default is selected', async () => {
  const user = userEvent.setup()
  const onSave = vi.fn()
  render(
    <ModelBindingDialog
      open
      onOpenChange={() => undefined}
      modelName='gpt-5.1'
      initialBinding={{
        model_name: 'gpt-5.1',
        enabled: false,
        probe_ids: ['p1'],
        reasoning_effort: 'off',
      }}
      probes={[probe]}
      onSave={onSave}
    />
  )

  await user.click(screen.getByRole('button', { name: 'Save' }))
  expect(onSave).toHaveBeenLastCalledWith({
    model_name: 'gpt-5.1',
    enabled: false,
    probe_ids: ['p1'],
    reasoning_effort: 'off',
    thinking_budget: undefined,
  })

  await user.click(screen.getByRole('combobox'))
  await user.click(screen.getByRole('option', { name: 'Use model default' }))
  await user.click(screen.getByRole('button', { name: 'Save' }))
  expect(onSave).toHaveBeenLastCalledWith({
    model_name: 'gpt-5.1',
    enabled: false,
    probe_ids: ['p1'],
    reasoning_effort: undefined,
    thinking_budget: undefined,
  })
})

test.each([
  { count: 1, probes: [probe], expectedProbeIds: undefined },
  {
    count: 2,
    probes: [probe, { ...probe, id: 'p2', name: 'Second probe' }],
    expectedProbeIds: ['p1'],
  },
])(
  'unchecking all $count probes blocks saving until a probe is selected',
  async ({ probes, expectedProbeIds }) => {
    const user = userEvent.setup()
    const onSave = vi.fn()
    const showError = vi.spyOn(toast, 'error')
    render(
      <ModelBindingDialog
        open
        onOpenChange={() => undefined}
        modelName='o3'
        initialBinding={null}
        probes={probes}
        onSave={onSave}
      />
    )

    for (const checkbox of screen.getAllByRole('checkbox')) {
      await user.click(checkbox)
    }
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(onSave).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('Select at least one probe')

    await user.click(screen.getAllByRole('checkbox')[0])
    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave).toHaveBeenCalledWith({
      model_name: 'o3',
      enabled: true,
      probe_ids: expectedProbeIds,
      reasoning_effort: undefined,
      thinking_budget: undefined,
    })
  }
)

test('selecting a probe after deleting a bound probe preserves the explicit subset', async () => {
  const user = userEvent.setup()
  const onSave = vi.fn()
  render(
    <ModelBindingDialog
      open
      onOpenChange={() => undefined}
      modelName='o3'
      initialBinding={{
        model_name: 'o3',
        enabled: true,
        probe_ids: ['deleted-probe'],
      }}
      probes={[probe, { ...probe, id: 'p2', name: 'Second probe' }]}
      onSave={onSave}
    />
  )

  const checkboxes = screen.getAllByRole('checkbox')
  expect(checkboxes[0]).not.toBeChecked()
  expect(checkboxes[1]).not.toBeChecked()
  await user.click(checkboxes[0])
  expect(checkboxes[1]).not.toBeChecked()
  await user.click(screen.getByRole('button', { name: 'Save' }))

  expect(onSave.mock.calls[0][0].probe_ids).toEqual(['p1'])
})

test('a binding containing only deleted probes requires a valid selection before saving', async () => {
  const user = userEvent.setup()
  const onSave = vi.fn()
  const showError = vi.spyOn(toast, 'error')
  render(
    <ModelBindingDialog
      open
      onOpenChange={() => undefined}
      modelName='o3'
      initialBinding={{
        model_name: 'o3',
        enabled: true,
        probe_ids: ['deleted-probe'],
      }}
      probes={[probe]}
      onSave={onSave}
    />
  )

  await user.click(screen.getByRole('button', { name: 'Save' }))

  expect(onSave).not.toHaveBeenCalled()
  expect(showError).toHaveBeenCalledWith('Select at least one probe')
})

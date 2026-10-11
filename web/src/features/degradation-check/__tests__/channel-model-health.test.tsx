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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test } from 'vitest'

import {
  ChannelModelCard,
  type ModelChannelInstance,
} from '../components/channel-model-card'

const clients: QueryClient[] = []

afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
})

function instance(
  totalExecutions: number,
  passRate = 100
): ModelChannelInstance {
  return {
    channelId: totalExecutions + 1,
    channelName: `Node ${totalExecutions}`,
    model: {
      model_name: 'test-model',
      enabled: true,
      total_executions: totalExecutions,
      pass_rate: passRate,
      avg_latency_ms: totalExecutions > 0 ? 800 : 0,
      probes: [
        {
          probe_id: 'logic',
          probe_name: 'Logic Probe',
          pass_rate: passRate,
          slots: [totalExecutions > 0 ? 1 : 0],
        },
      ],
    },
  }
}

function renderCard(instances: ModelChannelInstance[]) {
  const client = new QueryClient()
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>
      <ChannelModelCard modelName='test-model' instances={instances} />
    </QueryClientProvider>
  )
}

test('a model with zero executions shows no data instead of healthy or 100%', () => {
  renderCard([instance(0)])

  expect(screen.queryByText('Healthy')).toBeNull()
  expect(screen.queryAllByText(/100\.0%/)).toHaveLength(0)
  expect(screen.getAllByText('No Data')).toHaveLength(3)
})

test('an unexecuted node does not dilute the executed node health or latency', () => {
  renderCard([instance(5, 60), instance(0)])

  expect(screen.getByText('60.0%')).toBeVisible()
  expect(screen.getByText('Critical Issue')).toBeVisible()
  expect(screen.getAllByText('800ms')).toHaveLength(2)
  expect(screen.queryByText('100.0% Pass')).toBeNull()
})

test('a probe without occupied timeline slots shows no data on an executed model', () => {
  const node = instance(5)
  node.model.probes.push({
    probe_id: 'unused',
    probe_name: 'Unexecuted Probe',
    pass_rate: 100,
    slots: [0, 0],
  })
  renderCard([node])

  expect(screen.getByText('Healthy')).toBeVisible()
  expect(screen.getAllByText('100.0% Pass')).toHaveLength(1)
  expect(screen.getByText('No Data')).toBeVisible()
})

test('an aggregate timeline without executions shows no data', () => {
  const node = instance(0)
  node.model.probes = []
  renderCard([node])

  expect(screen.getByText('Aggregated Probes')).toBeVisible()
  expect(screen.queryAllByText(/100\.0%/)).toHaveLength(0)
})

test('an aggregate timeline retains measured pass rate when executions exist', () => {
  const node = instance(5, 60)
  node.model.probes = []
  renderCard([node])

  expect(screen.getByText('60.0% Pass')).toBeVisible()
})

test('the named toggle exposes its state and supports keyboard collapse and expansion', async () => {
  const user = userEvent.setup()
  renderCard([instance(5)])

  const toggle = screen.getByRole('button', { name: 'Collapse: test-model' })
  expect(toggle).toHaveAttribute('aria-expanded', 'true')
  const detailsId = toggle.getAttribute('aria-controls')
  expect(detailsId).toBeTruthy()
  expect(document.querySelector(`[id="${detailsId}"]`)).toContainElement(
    screen.getByText('Node 5')
  )

  await user.tab()
  expect(toggle).toHaveFocus()
  await user.keyboard('{Enter}')
  expect(toggle).toHaveAccessibleName('Expand: test-model')
  expect(toggle).toHaveAttribute('aria-expanded', 'false')
  expect(screen.queryByText('Node 5')).toBeNull()

  await user.keyboard(' ')
  expect(toggle).toHaveAccessibleName('Collapse: test-model')
  expect(toggle).toHaveAttribute('aria-expanded', 'true')
  expect(document.querySelector(`[id="${detailsId}"]`)).toContainElement(
    screen.getByText('Node 5')
  )
})

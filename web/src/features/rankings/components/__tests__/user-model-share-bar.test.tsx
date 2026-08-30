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
import { act, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import type { UserRankingModel } from '../../types'
import { UserModelShareBar } from '../user-model-share-bar'

const MODELS: UserRankingModel[] = [
  {
    model_name: 'model-a',
    total_tokens: 500,
    share: 0.5,
    is_other: false,
  },
  {
    model_name: 'model-b',
    total_tokens: 500,
    share: 0.5,
    is_other: false,
  },
]

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('user model share bar', () => {
  test('renders exact segment shares with every label below the bar', () => {
    render(<UserModelShareBar models={MODELS} />)

    const bar = screen.getByRole('img', {
      name: 'Model usage distribution',
    })
    const segments = bar.querySelectorAll<HTMLElement>('[data-share-segment]')
    const labels = screen.getByTestId('share-labels')

    expect([...segments].map((segment) => segment.style.width)).toEqual([
      '50%',
      '50%',
    ])
    expect(
      bar.compareDocumentPosition(labels) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(screen.getByText('model-a')).toBeInTheDocument()
    expect(screen.getAllByText('50.0%')).toHaveLength(2)
  })

  test('adds a label lane after a narrow resize causes a collision', async () => {
    let containerWidth = 400
    let resizeCallback: ResizeObserverCallback | undefined

    class ControlledResizeObserver {
      constructor(callback: ResizeObserverCallback) {
        resizeCallback = callback
      }
      observe(): void {}
      unobserve(): void {}
      disconnect(): void {}
    }

    vi.stubGlobal('ResizeObserver', ControlledResizeObserver)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(
      function (this: HTMLElement) {
        const width = this.hasAttribute('data-share-label')
          ? 100
          : containerWidth
        return {
          width,
          height: 20,
          top: 0,
          right: width,
          bottom: 20,
          left: 0,
          x: 0,
          y: 0,
          toJSON: () => ({}),
        }
      }
    )

    render(<UserModelShareBar models={MODELS} />)

    const labelsContainer = screen.getByTestId('share-labels')
    const labels =
      labelsContainer.querySelectorAll<HTMLElement>('[data-share-label]')

    await waitFor(() => {
      expect([...labels].map((label) => label.dataset.lane)).toEqual(['0', '0'])
    })
    const wideHeight = Number.parseFloat(labelsContainer.style.height)

    containerWidth = 200
    act(() => resizeCallback?.([], {} as ResizeObserver))

    await waitFor(() => {
      expect([...labels].map((label) => label.dataset.lane)).toEqual(['0', '1'])
    })
    expect(Number.parseFloat(labelsContainer.style.height)).toBeGreaterThan(
      wideHeight
    )
  })
})

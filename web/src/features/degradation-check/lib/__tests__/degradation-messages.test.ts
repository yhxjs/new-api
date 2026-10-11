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
import i18next from 'i18next'
import { beforeEach, describe, expect, test } from 'vitest'

import { localizeDegradationMessage } from '../degradation-messages'

describe('localizeDegradationMessage', () => {
  beforeEach(async () => {
    await i18next.init({
      lng: 'zh',
      resources: {
        zh: {
          translation: {
            'Degradation check is disabled': '降智检测未启用',
            'No probes configured': '未配置探针',
            'Channel not found': '未找到指定渠道',
            'Failed to test target': '测试目标失败',
          },
        },
      },
    })
  })

  test('localizes known backend message', () => {
    expect(localizeDegradationMessage('degradation check is disabled')).toBe(
      '降智检测未启用'
    )
  })

  test('handles case-insensitive and trimmed server messages', () => {
    expect(
      localizeDegradationMessage('  Degradation Check Is Disabled  ')
    ).toBe('降智检测未启用')
  })

  test('localizes other known errors', () => {
    expect(localizeDegradationMessage('no probes configured')).toBe(
      '未配置探针'
    )
    expect(localizeDegradationMessage('channel not found')).toBe(
      '未找到指定渠道'
    )
  })

  test('falls back to custom fallback if message is unknown', () => {
    expect(
      localizeDegradationMessage('some unknown backend error', '测试目标失败')
    ).toBe('测试目标失败')
  })

  test('falls back to rawMsg if unknown and no fallback provided', () => {
    expect(localizeDegradationMessage('custom unknown error')).toBe(
      'custom unknown error'
    )
  })

  test('handles undefined or empty input', () => {
    expect(localizeDegradationMessage(undefined, '默认提示')).toBe('默认提示')
    expect(localizeDegradationMessage('', '默认提示')).toBe('默认提示')
  })
})

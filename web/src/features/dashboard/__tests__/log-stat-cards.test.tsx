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
import { render, screen, waitFor } from '@testing-library/react'
import i18next from 'i18next'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import * as dashboardApi from '../api'
import { LogStatCards } from '../components/models/log-stat-cards'

vi.mock('../api', () => ({
  getUserQuotaDates: vi.fn(),
}))

describe('LogStatCards', () => {
  beforeAll(async () => {
    if (!i18next.isInitialized) {
      await i18next.init({
        lng: 'zh',
        resources: {
          zh: {
            translation: {
              'Total Count': '总调用次数',
              'Total Quota': '总消费额度',
              'Total Tokens': '总 Token 数',
              'Average RPM': '平均 RPM',
              'Average TPM': '平均 TPM',
              Input: '输入',
              Output: '输出',
              'Cache Hit Rate': '缓存命中率',
              Cache: '缓存',
            },
          },
        },
      })
    }
  })

  it('renders input, output, and cache hit rate alongside total tokens', async () => {
    vi.mocked(dashboardApi.getUserQuotaDates).mockResolvedValueOnce({
      success: true,
      data: [
        {
          created_at: 1000,
          quota: 50,
          count: 10,
          token_used: 1200,
          prompt_tokens: 1000,
          completion_tokens: 200,
          cache_tokens: 450,
        },
      ],
    })

    render(<LogStatCards />)

    await waitFor(() => {
      expect(screen.getByText(/Total Tokens|总 Token 数/)).toBeInTheDocument()
      expect(screen.getByText(/Input|输入/)).toBeInTheDocument()
      expect(screen.getByText(/Output|输出/)).toBeInTheDocument()
      expect(screen.getByText(/Cache Hit Rate|缓存命中率/)).toBeInTheDocument()
      expect(screen.getByText(/45%|45\.0%/)).toBeInTheDocument()
    })
  })
})

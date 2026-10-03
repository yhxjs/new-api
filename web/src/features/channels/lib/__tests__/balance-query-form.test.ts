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
import { describe, expect, test } from 'vitest'

import { CHANNEL_TYPE_NEW_API } from '../../constants'
import type { Channel } from '../../types'
import {
  BALANCE_QUERY_FORM_FIELDS,
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
} from '../channel-form'
import { isChannelBalanceQueryDisabled } from '../channel-utils'

function newAPIForm(
  overrides: Record<string, unknown> = {}
): Record<string, unknown> {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'New API upstream',
    type: CHANNEL_TYPE_NEW_API,
    base_url: 'https://new-api.example',
    key: 'sk-test-key',
    models: 'gpt-5',
    ...overrides,
  }
}

describe('balance query form validation', () => {
  test('a new custom request cannot use a redacted URL without saved settings', () => {
    const result = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'custom',
        balance_query_url: '[REDACTED]',
        balance_query_extract: 'response.balance',
      })
    )
    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.issues.map((issue) => issue.path[0])).toContain(
        'balance_query_url'
      )
    }
  })

  test('user_api mode requires user id; access token is only enforced on create in the drawer', () => {
    const missingBoth = channelFormSchema.safeParse(
      newAPIForm({ balance_query_mode: 'user_api' })
    )
    expect(missingBoth.success).toBe(false)
    if (!missingBoth.success) {
      const paths = missingBoth.error.issues.map((issue) => issue.path[0])
      expect(paths).not.toContain('balance_query_access_token')
      expect(paths).toContain('balance_query_user_id')
    }

    const complete = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'user_api',
        balance_query_access_token: 'pat-token',
        balance_query_user_id: '42',
      })
    )
    expect(complete.success).toBe(true)
  })

  test('custom mode requires a valid URL and extract expression', () => {
    const missingFields = channelFormSchema.safeParse(
      newAPIForm({ balance_query_mode: 'custom' })
    )
    expect(missingFields.success).toBe(false)
    if (!missingFields.success) {
      const paths = missingFields.error.issues.map((issue) => issue.path[0])
      expect(paths).toContain('balance_query_url')
      expect(paths).toContain('balance_query_extract')
    }

    const relativePath = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'custom',
        balance_query_url: '/api/balance',
        balance_query_extract: 'response.balance',
      })
    )
    expect(relativePath.success).toBe(true)

    // The {base_url} placeholder form matches the URL field placeholder and
    // must pass validation; it is expanded at query time.
    const baseURLPrefixed = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'custom',
        balance_query_url: '{base_url}/api/user/self',
        balance_query_extract: 'response.data.quota / 500000',
      })
    )
    expect(baseURLPrefixed.success).toBe(true)

    // A bare placeholder with no path is still invalid.
    const barePlaceholder = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'custom',
        balance_query_url: '{base_url}',
        balance_query_extract: 'response.balance',
      })
    )
    expect(barePlaceholder.success).toBe(false)

    const absoluteURL = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'custom',
        balance_query_url: 'https://other.example/balance',
        balance_query_extract: 'response.balance',
      })
    )
    expect(absoluteURL.success).toBe(true)

    const protocolRelative = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'custom',
        balance_query_url: '//evil.example/balance',
        balance_query_extract: 'response.balance',
      })
    )
    expect(protocolRelative.success).toBe(false)

    const invalidMethod = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'custom',
        balance_query_url: '/api/balance',
        balance_query_extract: 'response.balance',
        balance_query_method: 'DELETE',
      })
    )
    expect(invalidMethod.success).toBe(false)
    if (!invalidMethod.success) {
      expect(
        invalidMethod.error.issues.some(
          (issue) => issue.path[0] === 'balance_query_method'
        )
      ).toBe(true)
    }

    // A leftover body from switching POST back to GET must fail validation
    // instead of being silently submitted and rejected by the backend.
    const getWithBody = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'custom',
        balance_query_url: '/api/balance',
        balance_query_extract: 'response.balance',
        balance_query_method: 'GET',
        balance_query_body: '{"q":1}',
      })
    )
    expect(getWithBody.success).toBe(false)
    if (!getWithBody.success) {
      expect(
        getWithBody.error.issues.some(
          (issue) => issue.path[0] === 'balance_query_body'
        )
      ).toBe(true)
    }

    // Same for a lowercase method value typed directly into the form.
    const getWithBodyLowercase = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'custom',
        balance_query_url: '/api/balance',
        balance_query_extract: 'response.balance',
        balance_query_method: 'get',
        balance_query_body: '{"q":1}',
      })
    )
    expect(getWithBodyLowercase.success).toBe(false)
  })

  test('user_api mode rejects a negative quota per unit; zero normalizes to the default', () => {
    const negative = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'user_api',
        balance_query_access_token: 'pat-token',
        balance_query_user_id: '42',
        balance_query_quota_per_unit: -1,
      })
    )
    expect(negative.success).toBe(false)
    if (!negative.success) {
      expect(
        negative.error.issues.some(
          (issue) => issue.path[0] === 'balance_query_quota_per_unit'
        )
      ).toBe(true)
    }

    // Mirrors the backend MinBalanceQueryQuotaPerUnit bound: a near-zero
    // divisor would overflow the balance division into ±Inf.
    const nearZero = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'user_api',
        balance_query_access_token: 'pat-token',
        balance_query_user_id: '42',
        balance_query_quota_per_unit: 1e-300,
      })
    )
    expect(nearZero.success).toBe(false)
    if (!nearZero.success) {
      expect(
        nearZero.error.issues.some(
          (issue) => issue.path[0] === 'balance_query_quota_per_unit'
        )
      ).toBe(true)
    }

    // Mirrors the backend MaxBalanceQueryQuotaPerUnit bound: an absurd
    // divisor only shrinks the reported balance, but is a config mistake.
    const tooLarge = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'user_api',
        balance_query_access_token: 'pat-token',
        balance_query_user_id: '42',
        balance_query_quota_per_unit: 1e19,
      })
    )
    expect(tooLarge.success).toBe(false)
    if (!tooLarge.success) {
      expect(
        tooLarge.error.issues.some(
          (issue) => issue.path[0] === 'balance_query_quota_per_unit'
        )
      ).toBe(true)
    }

    // Zero mirrors the backend contract: unset, so it falls back to the
    // New API default (500000) and is omitted from the saved payload.
    const zero = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'user_api',
        balance_query_access_token: 'backend-supplies-token',
        balance_query_user_id: '42',
        balance_query_quota_per_unit: 0,
      })
    )
    expect(zero.success).toBe(true)
    if (zero.success) {
      const payload = transformFormDataToUpdatePayload(
        { ...zero.data, key: '' },
        1
      )
      const settings = JSON.parse(String(payload.settings))
      expect(settings.balance_query.quota_per_unit).toBeUndefined()
    }

    const valid = channelFormSchema.safeParse(
      newAPIForm({
        balance_query_mode: 'user_api',
        balance_query_access_token: 'pat-token',
        balance_query_user_id: '42',
        balance_query_quota_per_unit: 1000,
      })
    )
    expect(valid.success).toBe(true)
  })

  test('balance query settings are ignored for other channel types', () => {
    const result = channelFormSchema.safeParse(
      newAPIForm({ type: 1, balance_query_mode: 'user_api' })
    )
    expect(result.success).toBe(true)
  })
})

describe('balance query settings roundtrip', () => {
  function channelWithSettings(settings: Record<string, unknown>): Channel {
    return {
      id: 1,
      type: CHANNEL_TYPE_NEW_API,
      key: 'sk-test-key',
      name: 'New API upstream',
      status: 1,
      created_time: 0,
      test_time: 0,
      response_time: 0,
      base_url: 'https://new-api.example',
      models: 'gpt-5',
      group: 'default',
      balance: 0,
      balance_updated_time: 0,
      settings: JSON.stringify(settings),
      channel_info: {
        is_multi_key: false,
        multi_key_size: 0,
        multi_key_polling_index: 0,
        multi_key_mode: 'random',
      },
    } as unknown as Channel
  }

  test.each([
    {
      name: 'top-level and inner case aliases',
      settings:
        '{"BALANCE_QUERY":{"Mode":"custom","Method":"POST","URL":"[REDACTED]","Headers":{"Authorization":"[REDACTED]"},"Body":"[REDACTED]","Extract":"response.balance"}}',
      expected: {
        mode: 'custom',
        method: 'POST',
        url: '[REDACTED]',
        headers: { Authorization: '[REDACTED]' },
        body: '[REDACTED]',
        extract: 'response.balance',
      },
    },
    {
      name: 'multiple query objects merged in source order',
      settings:
        '{"balance_query":{"mode":"custom","url":"[REDACTED]","extract":"response.balance","headers":{"Authorization":"[REDACTED]"}},"Balance_Query":{"METHOD":"POST","BODY":"[REDACTED]","HEADERS":{"authorization":"[REDACTED]"}}}',
      expected: {
        mode: 'custom',
        method: 'POST',
        url: '[REDACTED]',
        headers: { Authorization: '[REDACTED]', authorization: '[REDACTED]' },
        body: '[REDACTED]',
        extract: 'response.balance',
      },
    },
    {
      name: 'duplicate query objects merged in source order',
      settings:
        '{"balance_query":{"mode":"custom","url":"[REDACTED]","extract":"response.balance"},"balance_query":{"method":"POST","body":"[REDACTED]"}}',
      expected: {
        mode: 'custom',
        method: 'POST',
        url: '[REDACTED]',
        body: '[REDACTED]',
        extract: 'response.balance',
      },
    },
    {
      name: 'duplicate scalar aliases keep the last non-null value',
      settings:
        '{"balance_query":{"mode":"user_api","Mode":"custom","mode":"user_api","MODE":null,"User_Id":"42","uſer_id":"43"}}',
      expected: { mode: 'user_api', user_id: '43' },
    },
    {
      name: 'a null query object resets earlier query fields',
      settings:
        '{"balance_query":{"mode":"custom","url":"[REDACTED]","extract":"response.balance"},"Balance_Query":null,"BALANCE_QUERY":{"Mode":"user_api","User_Id":"42"}}',
      expected: { mode: 'user_api', user_id: '42' },
    },
    {
      name: 'a null headers map resets earlier request headers',
      settings:
        '{"balance_query":{"mode":"custom","url":"[REDACTED]","extract":"response.balance","headers":{"Authorization":"[REDACTED]"},"HEADERS":null,"Headers":{"X-Token":"[REDACTED]"}}}',
      expected: {
        mode: 'custom',
        method: 'GET',
        url: '[REDACTED]',
        headers: { 'X-Token': '[REDACTED]' },
        extract: 'response.balance',
      },
    },
  ])('an unrelated edit preserves $name', (testCase) => {
    const channel = channelWithSettings({})
    channel.settings = testCase.settings
    const formValues = transformChannelToFormDefaults(channel)
    expect(formValues.balance_query_mode).toBe(testCase.expected.mode)
    if (testCase.expected.mode === 'custom') {
      expect(formValues.balance_query_url).toBe('[REDACTED]')
      expect(formValues.balance_query_extract).toBe('response.balance')
      expect(formValues.balance_query_method).toBe(testCase.expected.method)
      expect(JSON.parse(formValues.balance_query_headers || '{}')).toEqual(
        testCase.expected.headers || {}
      )
      expect(formValues.balance_query_body).toBe(testCase.expected.body || '')
    } else {
      expect(formValues.balance_query_user_id).toBe(testCase.expected.user_id)
    }
    const parsed = channelFormSchema.safeParse({
      ...formValues,
      key: '',
      name: 'Renamed upstream',
    })
    expect(parsed.success).toBe(true)
    if (!parsed.success) return
    const payload = transformFormDataToUpdatePayload(parsed.data, channel.id)
    expect(payload.settings).toContain(testCase.settings.slice(1, -1))
  })

  test('changing the balance query mode removes old aliases and custom credentials', () => {
    const channel = channelWithSettings({
      future: { enabled: true },
      balance_query: {
        Mode: 'custom',
        URL: '[REDACTED]',
        Extract: 'response.balance',
        future_query_field: 7,
      },
      Balance_Query: { Headers: { Authorization: '[REDACTED]' } },
    })
    const formValues = transformChannelToFormDefaults(channel)
    formValues.balance_query_mode = 'subscription'
    const payload = transformFormDataToUpdatePayload(formValues, channel.id)
    const settings = JSON.parse(String(payload.settings))
    expect(settings.balance_query).toEqual({ mode: 'subscription' })
    expect(settings.Balance_Query).toBeUndefined()
    expect(settings.future).toEqual({ enabled: true })
  })

  test('an existing redacted custom request can be submitted without exposing credentials', () => {
    const channel = channelWithSettings({
      balance_query: {
        mode: 'custom',
        method: 'POST',
        url: '[REDACTED]',
        headers: { Authorization: '[REDACTED]' },
        body: '[REDACTED]',
        extract: 'response.balance',
      },
    })
    const result = channelFormSchema.safeParse({
      ...transformChannelToFormDefaults(channel),
      key: '',
    })
    expect(result.success).toBe(true)
    if (result.success) {
      const payload = transformFormDataToUpdatePayload(result.data, channel.id)
      expect(JSON.parse(String(payload.settings)).balance_query).toEqual(
        JSON.parse(String(channel.settings)).balance_query
      )
    }
  })

  test('user_api settings survive channel -> form -> payload roundtrip', () => {
    const channel = channelWithSettings({
      balance_query: {
        mode: 'user_api',
        access_token: 'pat-token',
        user_id: '42',
        quota_per_unit: 1000,
      },
    })

    const formValues = transformChannelToFormDefaults(channel)
    expect(formValues.balance_query_mode).toBe('user_api')
    expect(formValues.balance_query_access_token).toBe('pat-token')
    expect(formValues.balance_query_user_id).toBe('42')
    expect(formValues.balance_query_quota_per_unit).toBe(1000)

    const payload = transformFormDataToUpdatePayload(
      { ...formValues, key: '' },
      channel.id
    )
    const settings = JSON.parse(String(payload.settings))
    expect(settings.balance_query).toEqual({
      mode: 'user_api',
      access_token: 'pat-token',
      user_id: '42',
      quota_per_unit: 1000,
    })
  })

  test('redacted user_api settings roundtrip omits the token so the backend keeps it', () => {
    // The API no longer returns balance_query.access_token to ChannelRead
    // admins; an unchanged edit must therefore submit user_api mode without
    // the token, which the backend merges back to the stored value.
    const channel = channelWithSettings({
      balance_query: {
        mode: 'user_api',
        user_id: '42',
      },
    })

    const formValues = transformChannelToFormDefaults(channel)
    expect(formValues.balance_query_mode).toBe('user_api')
    expect(formValues.balance_query_access_token).toBe('')

    const payload = transformFormDataToUpdatePayload(
      { ...formValues, key: '' },
      channel.id
    )
    const settings = JSON.parse(String(payload.settings))
    expect(settings.balance_query).toEqual({
      mode: 'user_api',
      user_id: '42',
    })
    expect(settings.balance_query.access_token).toBeUndefined()
  })

  test('custom settings roundtrip normalizes headers into JSON text', () => {
    const channel = channelWithSettings({
      balance_query: {
        mode: 'custom',
        method: 'POST',
        url: '/api/user/self',
        headers: { Authorization: 'Bearer {key}' },
        body: '{"key":"{key}"}',
        extract: 'response.data.quota / 500000',
      },
    })

    const formValues = transformChannelToFormDefaults(channel)
    expect(formValues.balance_query_mode).toBe('custom')
    expect(formValues.balance_query_method).toBe('POST')
    expect(formValues.balance_query_url).toBe('/api/user/self')
    expect(JSON.parse(String(formValues.balance_query_headers))).toEqual({
      Authorization: 'Bearer {key}',
    })

    const payload = transformFormDataToUpdatePayload(
      { ...formValues, key: '' },
      channel.id
    )
    const settings = JSON.parse(String(payload.settings))
    expect(settings.balance_query).toEqual({
      mode: 'custom',
      method: 'POST',
      url: '/api/user/self',
      headers: { Authorization: 'Bearer {key}' },
      body: '{"key":"{key}"}',
      extract: 'response.data.quota / 500000',
    })
  })

  test('disabled mode does not inject a default into legacy settings', () => {
    // Legacy New API channels never stored balance_query; the default
    // disabled mode must stay "not stored" instead of injecting
    // {"mode":"disabled"}, which the backend sensitivity check would
    // flag as a settings change on every unrelated edit.
    const formValues = transformChannelToFormDefaults(channelWithSettings({}))
    expect(formValues.balance_query_mode).toBe('disabled')

    const payload = transformFormDataToUpdatePayload(
      { ...formValues, key: '' },
      1
    )
    const settings = JSON.parse(String(payload.settings))
    expect(settings.balance_query).toBeUndefined()
  })

  test('isChannelBalanceQueryDisabled accurately classifies channels', () => {
    // Non-NewAPI channel
    expect(
      isChannelBalanceQueryDisabled({
        type: 1,
      } as unknown as Channel)
    ).toBe(false)

    // NewAPI channel without settings (defaults to disabled)
    expect(
      isChannelBalanceQueryDisabled({
        type: CHANNEL_TYPE_NEW_API,
      } as unknown as Channel)
    ).toBe(true)

    // NewAPI channel with explicit disabled mode
    expect(
      isChannelBalanceQueryDisabled({
        type: CHANNEL_TYPE_NEW_API,
        settings: JSON.stringify({ balance_query: { mode: 'disabled' } }),
      } as unknown as Channel)
    ).toBe(true)

    // NewAPI channel with subscription mode
    expect(
      isChannelBalanceQueryDisabled({
        type: CHANNEL_TYPE_NEW_API,
        settings: JSON.stringify({ balance_query: { mode: 'subscription' } }),
      } as unknown as Channel)
    ).toBe(false)

    // NewAPI channel with user_api mode
    expect(
      isChannelBalanceQueryDisabled({
        type: CHANNEL_TYPE_NEW_API,
        settings: JSON.stringify({
          balance_query: { mode: 'user_api', access_token: 't', user_id: '1' },
        }),
      } as unknown as Channel)
    ).toBe(false)
  })

  test.each([
    { name: 'null settings disable queries', settings: 'null', disabled: true },
    {
      name: 'invalid JSON disables queries',
      settings: '{',
      disabled: true,
    },
    {
      name: 'case aliases enable subscription queries',
      settings: '{"BALANCE_QUERY":{"Mode":"subscription"}}',
      disabled: false,
    },
    {
      name: 'duplicate objects preserve an enabled mode',
      settings:
        '{"balance_query":{"mode":"subscription"},"balance_query":{"quota_per_unit":1000}}',
      disabled: false,
    },
    {
      name: 'the last non-null scalar alias enables queries',
      settings:
        '{"balance_query":{"mode":"disabled","Mode":"subscription","MODE":null}}',
      disabled: false,
    },
    {
      name: 'a null query alias disables an earlier enabled mode',
      settings: '{"balance_query":{"mode":"user_api"},"Balance_Query":null}',
      disabled: true,
    },
    {
      name: 'an object after a null alias enables queries',
      settings:
        '{"balance_query":{"mode":"disabled"},"Balance_Query":null,"BALANCE_QUERY":{"Mode":"user_api","User_Id":"42"}}',
      disabled: false,
    },
    {
      name: 'a later mode alias disables queries',
      settings: '{"balance_query":{"mode":"user_api","Mode":"disabled"}}',
      disabled: true,
    },
  ])('balance query availability follows $name', (testCase) => {
    const channel = channelWithSettings({})
    channel.settings = testCase.settings

    expect(isChannelBalanceQueryDisabled(channel)).toBe(testCase.disabled)
  })

  test('an explicitly stored subscription entry survives an edit', () => {
    // A stored {"mode":"subscription"} must not be dropped by an edit: the
    // form round-trips it so the stored value stays stable.
    const channel = channelWithSettings({
      balance_query: { mode: 'subscription' },
    })
    const formValues = transformChannelToFormDefaults(channel)
    expect(formValues.balance_query_mode).toBe('subscription')

    const payload = transformFormDataToUpdatePayload(
      { ...formValues, key: '' },
      channel.id
    )
    const settings = JSON.parse(String(payload.settings))
    expect(settings.balance_query).toEqual({ mode: 'subscription' })
  })

  test('balance_query is removed when channel type changes away from New API', () => {
    const channel = channelWithSettings({
      balance_query: { mode: 'user_api', access_token: 't', user_id: '1' },
    })
    const formValues = transformChannelToFormDefaults(channel)
    formValues.type = 1

    const payload = transformFormDataToUpdatePayload(
      { ...formValues, key: '' },
      1
    )
    const settings = JSON.parse(String(payload.settings))
    expect(settings.balance_query).toBeUndefined()
  })

  test('every balance-query form field is classified for the sensitive lock', () => {
    // The backend treats every balance-query field — including the access
    // token — as ChannelSensitiveWrite-gated: an unchanged edit round-trips
    // the redacted settings and stays non-sensitive, while a changed token is
    // a credential change like the channel key. The drawer spreads this list
    // into SENSITIVE_FORM_FIELDS, so it must contain every balance_query_*
    // form field, token included.
    expect(BALANCE_QUERY_FORM_FIELDS).toEqual([
      'balance_query_mode',
      'balance_query_access_token',
      'balance_query_user_id',
      'balance_query_quota_per_unit',
      'balance_query_method',
      'balance_query_url',
      'balance_query_headers',
      'balance_query_body',
      'balance_query_extract',
    ])
  })

  test('a supplied token round-trips in user_api settings and is flagged for the backend', () => {
    // The settings payload shape is the same whoever edits: the redacted
    // settings from the API carry no token, a newly typed token replaces it,
    // and other balance-query fields round-trip unchanged. The backend
    // detects the token difference and requires ChannelSensitiveWrite.
    const channel = channelWithSettings({
      balance_query: { mode: 'user_api', user_id: '42' },
    })
    const formValues = transformChannelToFormDefaults(channel)
    formValues.balance_query_access_token = 'pat-rotated'

    const payload = transformFormDataToUpdatePayload(
      { ...formValues, key: '' },
      channel.id
    )
    const settings = JSON.parse(String(payload.settings))
    expect(settings.balance_query).toEqual({
      mode: 'user_api',
      access_token: 'pat-rotated',
      user_id: '42',
    })
  })
})

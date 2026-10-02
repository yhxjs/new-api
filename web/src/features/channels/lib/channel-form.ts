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
import { z } from 'zod'

import {
  CLAUDE_FIELD_PASSTHROUGH_TYPES,
  CHANNEL_TYPE_NEW_API,
  CHANNEL_TYPE_TASK_PLUGIN,
  CHANNEL_STATUS,
  ERROR_MESSAGES,
  FIELD_PASSTHROUGH_TYPES,
  MODEL_FETCHABLE_TYPES,
  OPENAI_FIELD_PASSTHROUGH_TYPES,
} from '../constants'
import type { Channel, ChannelBalanceQueryConfig } from '../types'
import {
  CHANNEL_TYPE_ADVANCED_CUSTOM,
  advancedCustomConfigUsesRelativeUpstreamPath,
  hasValidAdvancedCustomModelListRoute,
  parseAdvancedCustomConfig,
  stringifyAdvancedCustomConfig,
  validateAdvancedCustomConfig,
} from './advanced-custom'

// ============================================================================
// Form Validation Schema
// ============================================================================

const SUPPORTED_PROXY_PROTOCOLS = new Set([
  'http:',
  'https:',
  'socks5:',
  'socks5h:',
])

function isOptionalProxyURL(value: string | undefined): boolean {
  const trimmedValue = value?.trim() || ''
  if (!trimmedValue) return true

  const schemeSeparatorIndex = trimmedValue.indexOf('://')
  if (schemeSeparatorIndex <= 0) return false

  const authorityAndSuffix = trimmedValue.slice(schemeSeparatorIndex + 3)
  const suffixIndex = authorityAndSuffix.search(/[/?#]/)
  if (suffixIndex >= 0 && authorityAndSuffix.slice(suffixIndex) !== '/') {
    return false
  }

  try {
    const parsedURL = new URL(trimmedValue)
    return (
      SUPPORTED_PROXY_PROTOCOLS.has(parsedURL.protocol) &&
      Boolean(parsedURL.hostname) &&
      parsedURL.port !== '0'
    )
  } catch {
    return false
  }
}

export const HTTP_PROTOCOL_AUTO = 'auto'
export const HTTP_PROTOCOL_HTTP1 = 'http1'
export const MAX_HTTP2_CONNECTION_SHARDS = 8
export const REDACTED_BALANCE_QUERY_VALUE = '[REDACTED]'

export function normalizeHttpProtocol(
  value: string | undefined | null
): 'auto' | 'http1' {
  const normalized = String(value || '')
    .trim()
    .toLowerCase()
  if (normalized === HTTP_PROTOCOL_HTTP1) {
    return HTTP_PROTOCOL_HTTP1
  }
  return HTTP_PROTOCOL_AUTO
}

export function normalizeHttp2ConnectionShards(
  value: number | undefined | null
): number {
  if (value == null || Number.isNaN(value) || value === 0) {
    return 1
  }
  if (value < 1) {
    return 1
  }
  if (value > MAX_HTTP2_CONNECTION_SHARDS) {
    return MAX_HTTP2_CONNECTION_SHARDS
  }
  return value
}

function parseOptionalJson(value: string | undefined): unknown {
  if (!value?.trim()) return undefined
  return JSON.parse(value)
}

function isJsonObjectValue(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isOptionalJsonObject(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    return parsed === undefined || isJsonObjectValue(parsed)
  } catch {
    return false
  }
}

function isOptionalModelMapping(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (!isJsonObjectValue(parsed)) return false
    return Object.values(parsed).every((item) => typeof item === 'string')
  } catch {
    return false
  }
}

function isOptionalStatusCodeMapping(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (!isJsonObjectValue(parsed)) return false
    return Object.entries(parsed).every(([from, to]) => {
      const fromCode = Number(from)
      const toCode = Number(to)
      return (
        Number.isInteger(fromCode) &&
        Number.isInteger(toCode) &&
        fromCode >= 100 &&
        fromCode <= 599 &&
        toCode >= 100 &&
        toCode <= 599
      )
    })
  } catch {
    return false
  }
}

// Mirrors dto.MinBalanceQueryQuotaPerUnit on the backend: a near-zero
// quota_per_unit divisor would overflow the balance division into ±Inf.
// Zero itself means "use the default" and stays allowed.
const MIN_BALANCE_QUERY_QUOTA_PER_UNIT = 1e-9

// Mirrors dto.MaxBalanceQueryQuotaPerUnit on the backend: an oversized
// divisor only shrinks the reported balance, but rejecting it at save time
// surfaces configuration mistakes early.
const MAX_BALANCE_QUERY_QUOTA_PER_UNIT = 1e18

function isValidBalanceQueryCustomURL(value: string | undefined): boolean {
  let trimmed = value?.trim() || ''
  if (!trimmed) return false
  // "{base_url}/v1/balance" is the placeholder form of a relative path; the
  // placeholder is expanded against the channel base URL at query time.
  trimmed = trimmed.replace(/^\{base_url\}/, '')
  if (!trimmed) return false
  if (trimmed.startsWith('/') && !trimmed.startsWith('//')) return true
  try {
    const parsed = new URL(trimmed)
    return (
      (parsed.protocol === 'http:' || parsed.protocol === 'https:') &&
      Boolean(parsed.hostname)
    )
  } catch {
    return false
  }
}

// Whether the custom balance query URL resolves against the channel base URL
// ({base_url}/... or a plain /path), and so requires a base URL to be set.
function usesRelativeBalanceQueryURL(value: string | undefined): boolean {
  let trimmed = value?.trim() || ''
  if (!trimmed) return false
  trimmed = trimmed.replace(/^\{base_url\}/, '')
  return trimmed.startsWith('/') && !trimmed.startsWith('//')
}

function parseBalanceQueryHeaders(
  value: string | undefined
): Record<string, string> | null {
  if (!value?.trim()) return {}
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return {}
    if (!isJsonObjectValue(parsed)) return null
    const result: Record<string, string> = {}
    for (const [name, headerValue] of Object.entries(parsed)) {
      if (typeof headerValue !== 'string') return null
      if (name.trim()) result[name.trim()] = headerValue
    }
    return result
  } catch {
    return null
  }
}

function isCodexCredential(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    return (
      isJsonObjectValue(parsed) &&
      typeof parsed.access_token === 'string' &&
      parsed.access_token.trim().length > 0 &&
      typeof parsed.account_id === 'string' &&
      parsed.account_id.trim().length > 0
    )
  } catch {
    return false
  }
}

function isVertexJsonKey(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (Array.isArray(parsed)) {
      return parsed.every((item) => isJsonObjectValue(item))
    }
    return isJsonObjectValue(parsed)
  } catch {
    return false
  }
}

function addRequiredIssue(
  ctx: z.RefinementCtx,
  path: string,
  message: string
): void {
  ctx.addIssue({
    code: z.ZodIssueCode.custom,
    path: [path],
    message,
  })
}

export const channelFormSchema = z
  .object({
    name: z.string().min(1, ERROR_MESSAGES.REQUIRED_NAME),
    type: z.number().min(0, ERROR_MESSAGES.REQUIRED_TYPE),
    base_url: z.string().optional(),
    task_plugin_key: z.string().optional(),
    key: z.string(),
    openai_organization: z.string().optional(),
    models: z.string().min(1, ERROR_MESSAGES.REQUIRED_MODELS),
    group: z.array(z.string()).min(1, ERROR_MESSAGES.REQUIRED_GROUP),
    model_mapping: z
      .string()
      .optional()
      .refine(
        isOptionalModelMapping,
        'Model mapping must be a JSON object with string values'
      ),
    priority: z.number().optional(),
    weight: z.number().optional(),
    test_model: z.string().optional(),
    auto_ban: z.number().optional(),
    status: z.number(),
    status_code_mapping: z
      .string()
      .optional()
      .refine(
        isOptionalStatusCodeMapping,
        'Status code mapping must use valid HTTP status codes'
      ),
    tag: z.string().optional(),
    remark: z
      .string()
      .max(255, 'Remark must be less than 255 characters')
      .optional(),
    setting: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    param_override: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    header_override: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    settings: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    advanced_custom: z.string().optional(),
    other: z.string().optional(),
    // Multi-key options (not sent to backend directly)
    multi_key_mode: z.enum(['single', 'batch', 'multi_to_single']).optional(),
    multi_key_type: z.enum(['random', 'polling']).optional(),
    batch_add_set_key_prefix_2_name: z.boolean().optional(),
    key_mode: z.enum(['append', 'replace']).optional(), // For editing multi-key channels
    // Channel extra settings (stored in setting JSON, not sent directly)
    force_format: z.boolean().optional(),
    thinking_to_content: z.boolean().optional(),
    proxy: z
      .string()
      .optional()
      .refine(isOptionalProxyURL, ERROR_MESSAGES.INVALID_PROXY),
    http_protocol: z.enum(['auto', 'http1']).optional(),
    http2_connection_shards: z.number().int().optional(),
    pass_through_body_enabled: z.boolean().optional(),
    system_prompt: z.string().optional(),
    system_prompt_override: z.boolean().optional(),
    // Type-specific settings (stored in settings JSON)
    is_enterprise_account: z.boolean().optional(), // OpenRouter specific
    vertex_key_type: z.enum(['json', 'api_key']).optional(), // Vertex AI specific
    aws_key_type: z.enum(['ak_sk', 'api_key']).optional(), // AWS specific
    azure_responses_version: z.string().optional(), // Azure specific
    // Field passthrough controls (stored in settings JSON)
    allow_service_tier: z.boolean().optional(), // OpenAI/Anthropic
    disable_store: z.boolean().optional(), // OpenAI only
    allow_safety_identifier: z.boolean().optional(), // OpenAI only
    allow_include_obfuscation: z.boolean().optional(), // OpenAI: include usage obfuscation
    allow_inference_geo: z.boolean().optional(), // OpenAI/Anthropic: inference geography
    allow_speed: z.boolean().optional(), // Anthropic: speed mode control
    claude_beta_query: z.boolean().optional(), // Anthropic: beta query passthrough
    disable_task_polling_sleep: z.boolean().optional(),
    // Upstream model update settings (stored in settings JSON)
    upstream_model_update_check_enabled: z.boolean().optional(),
    upstream_model_update_auto_sync_enabled: z.boolean().optional(),
    upstream_model_update_ignored_models: z.string().optional(),
    // Balance query settings (stored in settings JSON, New API channels)
    balance_query_mode: z
      .enum(['subscription', 'user_api', 'custom'])
      .optional(),
    balance_query_access_token: z.string().optional(),
    balance_query_user_id: z.string().optional(),
    balance_query_quota_per_unit: z.number().optional(),
    balance_query_method: z.string().optional(),
    balance_query_url: z.string().optional(),
    balance_query_headers: z.string().optional(),
    balance_query_body: z.string().optional(),
    balance_query_extract: z.string().optional(),
  })
  .superRefine((data, ctx) => {
    if (
      [3, 8, 36, 45, CHANNEL_TYPE_NEW_API, CHANNEL_TYPE_TASK_PLUGIN].includes(
        data.type
      ) &&
      !data.base_url?.trim()
    ) {
      addRequiredIssue(
        ctx,
        'base_url',
        'Base URL is required for this channel type'
      )
    }
    if (
      data.type === CHANNEL_TYPE_TASK_PLUGIN &&
      !data.task_plugin_key?.trim()
    ) {
      addRequiredIssue(ctx, 'task_plugin_key', 'Task plugin is required')
    }

    if (data.type === CHANNEL_TYPE_ADVANCED_CUSTOM) {
      const advancedCustomConfig = parseAdvancedCustomConfig(
        data.advanced_custom
      )
      const advancedCustomError =
        validateAdvancedCustomConfig(advancedCustomConfig)
      if (advancedCustomError) {
        addRequiredIssue(ctx, 'advanced_custom', advancedCustomError.message)
      }
      if (
        advancedCustomConfigUsesRelativeUpstreamPath(advancedCustomConfig) &&
        !data.base_url?.trim()
      ) {
        addRequiredIssue(
          ctx,
          'base_url',
          'Base URL is required when an advanced route uses an upstream path'
        )
      }
      if (
        data.upstream_model_update_check_enabled === true &&
        !hasValidAdvancedCustomModelListRoute(advancedCustomConfig)
      ) {
        addRequiredIssue(
          ctx,
          'upstream_model_update_check_enabled',
          'OpenAI Models route is required to enable upstream model checks'
        )
      }
    }

    if ([3, 18, 21, 39, 41, 49].includes(data.type) && !data.other?.trim()) {
      addRequiredIssue(
        ctx,
        'other',
        'This channel type requires additional configuration'
      )
    }

    if (data.type === 57) {
      if (data.multi_key_mode && data.multi_key_mode !== 'single') {
        addRequiredIssue(
          ctx,
          'multi_key_mode',
          'Codex channels do not support batch creation'
        )
      }
      if (data.key?.trim() && !isCodexCredential(data.key)) {
        addRequiredIssue(
          ctx,
          'key',
          'Codex credential must be a JSON object with access_token and account_id'
        )
      }
    }

    if (
      data.type === 41 &&
      data.vertex_key_type === 'json' &&
      data.key?.trim() &&
      !isVertexJsonKey(data.key)
    ) {
      addRequiredIssue(
        ctx,
        'key',
        'Vertex AI service account key must be valid JSON'
      )
    }

    if (
      data.type === 41 &&
      data.vertex_key_type === 'api_key' &&
      data.multi_key_mode &&
      data.multi_key_mode !== 'single'
    ) {
      addRequiredIssue(
        ctx,
        'multi_key_mode',
        'Vertex AI API Key mode does not support batch creation'
      )
    }

    if (data.type === CHANNEL_TYPE_NEW_API) {
      if (data.balance_query_mode === 'user_api') {
        // The access token is intentionally not required here: creating is
        // enforced in the drawer submit handler (like the channel key), and
        // editing keeps an empty token as "keep existing".
        if (!data.balance_query_user_id?.trim()) {
          addRequiredIssue(
            ctx,
            'balance_query_user_id',
            'User ID is required for user API balance queries'
          )
        }
      }
      if (data.balance_query_mode === 'custom') {
        let keepsRedactedURL = false
        if (data.balance_query_url === REDACTED_BALANCE_QUERY_VALUE) {
          try {
            const savedQuery = parseBalanceQueryConfig(data.settings || '{}')
            keepsRedactedURL =
              savedQuery?.mode === 'custom' &&
              savedQuery.url === REDACTED_BALANCE_QUERY_VALUE
          } catch {
            // Invalid settings are reported by the settings field validator.
          }
        }
        if (
          !data.balance_query_url?.trim() ||
          (!keepsRedactedURL &&
            !isValidBalanceQueryCustomURL(data.balance_query_url))
        ) {
          addRequiredIssue(
            ctx,
            'balance_query_url',
            'Balance query URL must be a full http(s) URL or start with {base_url} or /'
          )
        }
        // Mirror the backend save-time guard: a relative URL resolves against
        // the channel base URL at query time, so it cannot work without one.
        if (
          isValidBalanceQueryCustomURL(data.balance_query_url) &&
          usesRelativeBalanceQueryURL(data.balance_query_url) &&
          !data.base_url?.trim()
        ) {
          addRequiredIssue(
            ctx,
            'base_url',
            'Base URL is required when the balance query URL is relative'
          )
        }
        if (!data.balance_query_extract?.trim()) {
          addRequiredIssue(
            ctx,
            'balance_query_extract',
            'Extract expression is required for custom balance queries'
          )
        }
        const method = data.balance_query_method?.trim().toUpperCase() || 'GET'
        if (method !== 'GET' && method !== 'POST') {
          addRequiredIssue(
            ctx,
            'balance_query_method',
            'Balance query method must be GET or POST'
          )
        }
        if (method === 'GET' && Boolean(data.balance_query_body?.trim())) {
          addRequiredIssue(
            ctx,
            'balance_query_body',
            'Balance query body is only allowed for POST requests'
          )
        }
        if (parseBalanceQueryHeaders(data.balance_query_headers) === null) {
          addRequiredIssue(
            ctx,
            'balance_query_headers',
            'Balance query headers must be a JSON object with string values'
          )
        }
      }
      if (
        data.balance_query_mode === 'user_api' &&
        data.balance_query_quota_per_unit != null &&
        data.balance_query_quota_per_unit < 0
      ) {
        addRequiredIssue(
          ctx,
          'balance_query_quota_per_unit',
          'Quota per USD must not be negative'
        )
      }
      // Zero passes (backend falls back to its default); a near-zero divisor
      // overflows the balance division and the backend rejects it at save.
      if (
        data.balance_query_mode === 'user_api' &&
        data.balance_query_quota_per_unit != null &&
        data.balance_query_quota_per_unit > 0 &&
        data.balance_query_quota_per_unit < MIN_BALANCE_QUERY_QUOTA_PER_UNIT
      ) {
        addRequiredIssue(
          ctx,
          'balance_query_quota_per_unit',
          'Quota per USD must be at least 0.000000001'
        )
      }
      // Mirrors the backend MaxBalanceQueryQuotaPerUnit bound: an absurd
      // divisor only shrinks the reported balance, but is a config mistake.
      if (
        data.balance_query_mode === 'user_api' &&
        data.balance_query_quota_per_unit != null &&
        data.balance_query_quota_per_unit > MAX_BALANCE_QUERY_QUOTA_PER_UNIT
      ) {
        addRequiredIssue(
          ctx,
          'balance_query_quota_per_unit',
          'Quota per USD must be at most 1e+18'
        )
      }
    }

    const protocol = normalizeHttpProtocol(data.http_protocol)
    const shards = data.http2_connection_shards ?? 1
    if (shards < 1 || shards > MAX_HTTP2_CONNECTION_SHARDS) {
      addRequiredIssue(
        ctx,
        'http2_connection_shards',
        ERROR_MESSAGES.INVALID_HTTP2_CONNECTION_SHARDS
      )
    }
    if (protocol === HTTP_PROTOCOL_HTTP1 && shards > 1) {
      addRequiredIssue(
        ctx,
        'http2_connection_shards',
        ERROR_MESSAGES.INVALID_HTTP1_WITH_SHARDS
      )
    }
  })

export type ChannelFormValues = z.infer<typeof channelFormSchema>

// ============================================================================
// Default Form Values
// ============================================================================

export const CHANNEL_FORM_DEFAULT_VALUES: ChannelFormValues = {
  name: '',
  type: 1,
  base_url: '',
  task_plugin_key: '',
  key: '',
  openai_organization: '',
  models: '',
  group: ['default'],
  model_mapping: '',
  priority: 0,
  weight: 0,
  test_model: '',
  auto_ban: 1,
  status: CHANNEL_STATUS.ENABLED,
  status_code_mapping: '',
  tag: '',
  remark: '',
  setting: '',
  param_override: '',
  header_override: '',
  settings: '{}',
  other: '',
  multi_key_mode: 'single',
  multi_key_type: 'random',
  batch_add_set_key_prefix_2_name: false,
  key_mode: 'append',
  // Channel extra settings
  force_format: false,
  thinking_to_content: false,
  proxy: '',
  http_protocol: HTTP_PROTOCOL_AUTO,
  http2_connection_shards: 1,
  pass_through_body_enabled: false,
  system_prompt: '',
  system_prompt_override: false,
  // Type-specific settings
  is_enterprise_account: false,
  vertex_key_type: 'json',
  aws_key_type: 'ak_sk',
  azure_responses_version: '',
  // Field passthrough controls
  allow_service_tier: false,
  disable_store: false,
  allow_safety_identifier: false,
  allow_include_obfuscation: false,
  allow_inference_geo: false,
  allow_speed: false,
  claude_beta_query: false,
  disable_task_polling_sleep: false,
  upstream_model_update_check_enabled: false,
  upstream_model_update_auto_sync_enabled: false,
  upstream_model_update_ignored_models: '',
  advanced_custom: '',
  // Balance query settings
  balance_query_mode: 'subscription',
  balance_query_access_token: '',
  balance_query_user_id: '',
  balance_query_quota_per_unit: undefined,
  balance_query_method: 'GET',
  balance_query_url: '',
  balance_query_headers: '',
  balance_query_body: '',
  balance_query_extract: '',
}

/**
 * All balance-query form fields. Every one of them — including the access
 * token — is gated by ChannelSensitiveWrite: the backend redacts
 * access tokens and custom request values from channel responses and restores
 * them server-side on submit. An unchanged edit stays non-sensitive, while
 * supplying a different credential requires the same policy as the channel key.
 */
export const BALANCE_QUERY_FORM_FIELDS = [
  'balance_query_mode',
  'balance_query_access_token',
  'balance_query_user_id',
  'balance_query_quota_per_unit',
  'balance_query_method',
  'balance_query_url',
  'balance_query_headers',
  'balance_query_body',
  'balance_query_extract',
] satisfies (keyof ChannelFormValues)[]

function foldBalanceQueryField(name: string): string {
  return name.replaceAll('ſ', 's').replaceAll('K', 'k').toLowerCase()
}

// Slice direct members of a validated object. JSON.parse handles each value;
// retaining the source preserves duplicate members and Go DTO merge order.
function settingsObjectMembers(source: string) {
  if (!isJsonObjectValue(JSON.parse(source))) return []
  const members: Array<{
    name: string
    value: unknown
    rawValue: string
    source: string
  }> = []
  let depth = 0
  let name: string | null = null
  let keyStart = 0
  let valueStart = 0
  for (const match of source.matchAll(/"(?:\\.|[^"\\])*"|[{}[\]:,]/g)) {
    const token = match[0]
    if (token === '{' || token === '[') depth++
    if (token === '}' || token === ']') depth--
    if (depth === 1 && token.startsWith('"') && name === null) {
      name = JSON.parse(token)
      keyStart = match.index
    } else if (depth === 1 && token === ':') {
      valueStart = match.index + 1
    } else if (
      name !== null &&
      ((depth === 1 && token === ',') || (depth === 0 && token === '}'))
    ) {
      const rawValue = source.slice(valueStart, match.index).trim()
      members.push({
        name,
        value: JSON.parse(rawValue),
        rawValue,
        source: source.slice(keyStart, match.index),
      })
      name = null
    }
  }
  return members
}

function parseBalanceQueryConfig(
  settings: string
): Record<string, unknown> | undefined {
  let query: Record<string, unknown> | undefined
  const fields = new Set(
    BALANCE_QUERY_FORM_FIELDS.map((field) =>
      field.slice('balance_query_'.length)
    )
  )
  for (const member of settingsObjectMembers(settings)) {
    if (foldBalanceQueryField(member.name) !== 'balance_query') continue
    if (member.value === null) {
      query = undefined
      continue
    }
    if (!isJsonObjectValue(member.value)) continue
    query ??= {}
    for (const field of settingsObjectMembers(member.rawValue)) {
      const name = foldBalanceQueryField(field.name)
      if (!fields.has(name)) continue
      if (name === 'headers') {
        if (field.value === null) delete query.headers
        else if (isJsonObjectValue(field.value)) {
          const headers = isJsonObjectValue(query.headers) ? query.headers : {}
          query.headers = {
            ...headers,
            ...Object.fromEntries(
              Object.entries(field.value).map(([key, value]) => [
                key,
                value ?? '',
              ])
            ),
          }
        }
      } else if (field.value !== null) {
        query[name] = field.value
      }
    }
  }
  return query
}

function balanceQueryFormDefaults(query?: Record<string, unknown>) {
  const config = query as ChannelBalanceQueryConfig | undefined
  let mode: 'subscription' | 'user_api' | 'custom' = 'subscription'
  if (config?.mode === 'user_api' || config?.mode === 'custom') {
    mode = config.mode
  }
  return {
    balance_query_mode: mode,
    balance_query_access_token: config?.access_token || '',
    balance_query_user_id: String(config?.user_id ?? ''),
    balance_query_quota_per_unit:
      config?.quota_per_unit && config.quota_per_unit > 0
        ? config.quota_per_unit
        : undefined,
    balance_query_method: config?.method || 'GET',
    balance_query_url: config?.url || '',
    balance_query_headers: config?.headers
      ? JSON.stringify(config.headers, null, 2)
      : '',
    balance_query_body: config?.body || '',
    balance_query_extract: config?.extract || '',
  }
}

// ============================================================================
// Transform Functions
// ============================================================================

/**
 * Transform Channel from API to Form default values
 */
export function transformChannelToFormDefaults(
  channel: Channel
): ChannelFormValues {
  // Parse channel extra settings from setting field
  let extraSettings = {
    task_plugin_key: '',
    force_format: false,
    thinking_to_content: false,
    proxy: '',
    http_protocol: HTTP_PROTOCOL_AUTO as 'auto' | 'http1',
    http2_connection_shards: 1,
    pass_through_body_enabled: false,
    system_prompt: '',
    system_prompt_override: false,
  }

  if (channel.setting) {
    try {
      const parsed = JSON.parse(channel.setting)
      const protocol = normalizeHttpProtocol(parsed.http_protocol)
      const shards = normalizeHttp2ConnectionShards(
        parsed.http2_connection_shards
      )
      extraSettings = {
        task_plugin_key: parsed.task_plugin_key || '',
        force_format: parsed.force_format || false,
        thinking_to_content: parsed.thinking_to_content || false,
        proxy: parsed.proxy || '',
        http_protocol: protocol,
        http2_connection_shards: protocol === HTTP_PROTOCOL_HTTP1 ? 1 : shards,
        pass_through_body_enabled: parsed.pass_through_body_enabled || false,
        system_prompt: parsed.system_prompt || '',
        system_prompt_override: parsed.system_prompt_override || false,
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse channel setting:', error)
    }
  }

  // Parse type-specific settings from settings field
  let vertexKeyType: 'json' | 'api_key' = 'json'
  let azureResponsesVersion = ''
  let isEnterpriseAccount = false
  let awsKeyType: 'ak_sk' | 'api_key' = 'ak_sk'
  let allowServiceTier = false
  let disableStore = false
  let allowSafetyIdentifier = false
  let allowIncludeObfuscation = false
  let allowInferenceGeo = false
  let allowSpeed = false
  let claudeBetaQuery = false
  let disableTaskPollingSleep = false
  let upstreamModelUpdateCheckEnabled = false
  let upstreamModelUpdateAutoSyncEnabled = false
  let upstreamModelUpdateIgnoredModels = ''
  let advancedCustom = ''
  let balanceQueryDefaults = balanceQueryFormDefaults()

  if (channel.settings) {
    try {
      const parsed = JSON.parse(channel.settings)
      vertexKeyType = parsed.vertex_key_type || 'json'
      azureResponsesVersion = parsed.azure_responses_version || ''
      isEnterpriseAccount = parsed.openrouter_enterprise === true
      awsKeyType = parsed.aws_key_type || 'ak_sk'
      allowServiceTier = parsed.allow_service_tier === true
      disableStore = parsed.disable_store === true
      allowSafetyIdentifier = parsed.allow_safety_identifier === true
      allowIncludeObfuscation = parsed.allow_include_obfuscation === true
      allowInferenceGeo = parsed.allow_inference_geo === true
      allowSpeed = parsed.allow_speed === true
      claudeBetaQuery = parsed.claude_beta_query === true
      disableTaskPollingSleep = parsed.disable_task_polling_sleep === true
      upstreamModelUpdateCheckEnabled =
        parsed.upstream_model_update_check_enabled === true
      upstreamModelUpdateAutoSyncEnabled =
        parsed.upstream_model_update_auto_sync_enabled === true
      upstreamModelUpdateIgnoredModels = Array.isArray(
        parsed.upstream_model_update_ignored_models
      )
        ? parsed.upstream_model_update_ignored_models.join(',')
        : ''
      if (parsed.advanced_custom) {
        advancedCustom = stringifyAdvancedCustomConfig(parsed.advanced_custom)
      }
      balanceQueryDefaults = balanceQueryFormDefaults(
        parseBalanceQueryConfig(channel.settings)
      )
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse channel settings:', error)
    }
  }

  return {
    name: channel.name || '',
    type: channel.type,
    base_url: channel.base_url || '',
    key: '', // Never populate key from backend for security
    openai_organization: channel.openai_organization || '',
    models: channel.models || '',
    group: parseGroups(channel.group || 'default'),
    model_mapping: channel.model_mapping || '',
    priority: channel.priority || 0,
    weight: channel.weight || 0,
    test_model: channel.test_model || '',
    auto_ban: channel.auto_ban ?? 1,
    status: channel.status,
    status_code_mapping: channel.status_code_mapping || '',
    tag: channel.tag || '',
    remark: channel.remark || '',
    setting: channel.setting || '',
    param_override: channel.param_override || '',
    header_override: channel.header_override || '',
    settings: channel.settings || '{}',
    other: channel.other || '',
    multi_key_mode: 'single',
    multi_key_type: channel.channel_info.multi_key_mode || 'random',
    batch_add_set_key_prefix_2_name: false,
    key_mode: 'append', // Default to append mode for editing multi-key channels
    // Channel extra settings
    ...extraSettings,
    // Type-specific settings
    is_enterprise_account: isEnterpriseAccount,
    vertex_key_type: vertexKeyType,
    azure_responses_version: azureResponsesVersion,
    aws_key_type: awsKeyType,
    allow_service_tier: allowServiceTier,
    disable_store: disableStore,
    allow_include_obfuscation: allowIncludeObfuscation,
    allow_inference_geo: allowInferenceGeo,
    allow_speed: allowSpeed,
    claude_beta_query: claudeBetaQuery,
    disable_task_polling_sleep: disableTaskPollingSleep,
    allow_safety_identifier: allowSafetyIdentifier,
    upstream_model_update_check_enabled: upstreamModelUpdateCheckEnabled,
    upstream_model_update_auto_sync_enabled: upstreamModelUpdateAutoSyncEnabled,
    upstream_model_update_ignored_models: upstreamModelUpdateIgnoredModels,
    advanced_custom: advancedCustom,
    ...balanceQueryDefaults,
  }
}

/**
 * Build the setting JSON string from form extra settings
 */
export function buildSettingJSON(formData: ChannelFormValues): string {
  const settingObj: Record<string, unknown> = {
    task_plugin_key:
      formData.type === CHANNEL_TYPE_TASK_PLUGIN
        ? formData.task_plugin_key?.trim() || ''
        : undefined,
    force_format: formData.force_format || false,
    thinking_to_content: formData.thinking_to_content || false,
    proxy: formData.proxy?.trim() || '',
    pass_through_body_enabled: formData.pass_through_body_enabled || false,
    system_prompt: formData.system_prompt || '',
    system_prompt_override: formData.system_prompt_override || false,
  }

  const protocol = normalizeHttpProtocol(formData.http_protocol)
  const shards =
    protocol === HTTP_PROTOCOL_HTTP1
      ? 1
      : normalizeHttp2ConnectionShards(formData.http2_connection_shards)

  // Omit defaults so unchanged channels keep equivalent JSON.
  if (protocol === HTTP_PROTOCOL_HTTP1) {
    settingObj.http_protocol = HTTP_PROTOCOL_HTTP1
  } else if (shards > 1) {
    settingObj.http2_connection_shards = shards
  }

  return JSON.stringify(settingObj)
}

/**
 * Build the settings JSON string (for type-specific config like vertex_key_type)
 */
function buildSettingsJSON(formData: ChannelFormValues): string {
  let settingsObj: Record<string, unknown> = {}
  let originalBalanceQuery = balanceQueryFormDefaults()
  let balanceQueryMembers: string[] = []

  // Try to parse existing settings first
  if (formData.settings && formData.settings !== '{}') {
    try {
      settingsObj = JSON.parse(formData.settings)
      originalBalanceQuery = balanceQueryFormDefaults(
        parseBalanceQueryConfig(formData.settings)
      )
      balanceQueryMembers = settingsObjectMembers(formData.settings)
        .filter(
          (member) => foldBalanceQueryField(member.name) === 'balance_query'
        )
        .map((member) => member.source)
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse existing settings:', error)
    }
  }

  // Add vertex_key_type for Vertex AI channels (type 41)
  if (formData.type === 41) {
    settingsObj.vertex_key_type = formData.vertex_key_type || 'json'
  } else if ('vertex_key_type' in settingsObj) {
    delete settingsObj.vertex_key_type
  }

  // Add azure_responses_version for Azure channels (type 3)
  if (formData.type === 3 && formData.azure_responses_version) {
    settingsObj.azure_responses_version = formData.azure_responses_version
  } else if ('azure_responses_version' in settingsObj) {
    delete settingsObj.azure_responses_version
  }

  // Add enterprise account setting for OpenRouter (type 20)
  if (formData.type === 20) {
    settingsObj.openrouter_enterprise = formData.is_enterprise_account === true
  } else if ('openrouter_enterprise' in settingsObj) {
    delete settingsObj.openrouter_enterprise
  }

  // Add aws_key_type for AWS channels (type 33)
  if (formData.type === 33) {
    settingsObj.aws_key_type = formData.aws_key_type || 'ak_sk'
  } else if ('aws_key_type' in settingsObj) {
    delete settingsObj.aws_key_type
  }

  // Field passthrough controls:
  // - OpenAI, Anthropic, Codex, and New API: allow_service_tier
  // - OpenAI request fields: OpenAI, Codex, and New API
  // - Claude request fields: Anthropic and New API
  if (FIELD_PASSTHROUGH_TYPES.has(formData.type)) {
    settingsObj.allow_service_tier = formData.allow_service_tier === true
  } else if ('allow_service_tier' in settingsObj) {
    delete settingsObj.allow_service_tier
  }

  if (OPENAI_FIELD_PASSTHROUGH_TYPES.has(formData.type)) {
    settingsObj.disable_store = formData.disable_store === true
    settingsObj.allow_safety_identifier =
      formData.allow_safety_identifier === true
    settingsObj.allow_include_obfuscation =
      formData.allow_include_obfuscation === true
  } else {
    if ('disable_store' in settingsObj) {
      delete settingsObj.disable_store
    }
    if ('allow_safety_identifier' in settingsObj) {
      delete settingsObj.allow_safety_identifier
    }
    if ('allow_include_obfuscation' in settingsObj) {
      delete settingsObj.allow_include_obfuscation
    }
  }

  if (
    OPENAI_FIELD_PASSTHROUGH_TYPES.has(formData.type) ||
    CLAUDE_FIELD_PASSTHROUGH_TYPES.has(formData.type)
  ) {
    settingsObj.allow_inference_geo = formData.allow_inference_geo === true
  } else if ('allow_inference_geo' in settingsObj) {
    delete settingsObj.allow_inference_geo
  }

  if (CLAUDE_FIELD_PASSTHROUGH_TYPES.has(formData.type)) {
    settingsObj.allow_speed = formData.allow_speed === true
  } else if ('allow_speed' in settingsObj) {
    delete settingsObj.allow_speed
  }

  // Only the Anthropic adaptor supports forcing the Claude beta query.
  if (formData.type === 14) {
    settingsObj.claude_beta_query = formData.claude_beta_query === true
  } else if ('claude_beta_query' in settingsObj) {
    delete settingsObj.claude_beta_query
  }

  settingsObj.disable_task_polling_sleep =
    formData.disable_task_polling_sleep === true

  // Upstream model update settings (for model-fetchable channel types)
  if (MODEL_FETCHABLE_TYPES.has(formData.type)) {
    settingsObj.upstream_model_update_check_enabled =
      formData.upstream_model_update_check_enabled === true
    settingsObj.upstream_model_update_auto_sync_enabled =
      settingsObj.upstream_model_update_check_enabled === true &&
      formData.upstream_model_update_auto_sync_enabled === true
    settingsObj.upstream_model_update_ignored_models = [
      ...new Set(
        String(formData.upstream_model_update_ignored_models || '')
          .split(',')
          .map((model) => model.trim())
          .filter(Boolean)
      ),
    ]
    if (
      !Array.isArray(settingsObj.upstream_model_update_last_detected_models) ||
      settingsObj.upstream_model_update_check_enabled !== true
    ) {
      settingsObj.upstream_model_update_last_detected_models = []
    }
    if (typeof settingsObj.upstream_model_update_last_check_time !== 'number') {
      settingsObj.upstream_model_update_last_check_time = 0
    }
  }

  if (formData.type === CHANNEL_TYPE_ADVANCED_CUSTOM) {
    const advancedCustomConfig = parseAdvancedCustomConfig(
      formData.advanced_custom
    )
    if (advancedCustomConfig) {
      settingsObj.advanced_custom = advancedCustomConfig
    }
  } else if ('advanced_custom' in settingsObj) {
    delete settingsObj.advanced_custom
  }

  // Balance query settings for New API channels
  if (formData.type === CHANNEL_TYPE_NEW_API) {
    const mode = formData.balance_query_mode || 'subscription'
    const balanceQuery: Record<string, unknown> = { mode }
    if (mode === 'user_api') {
      const accessToken = formData.balance_query_access_token?.trim() || ''
      const userId = formData.balance_query_user_id?.trim() || ''
      if (accessToken) balanceQuery.access_token = accessToken
      if (userId) balanceQuery.user_id = userId
      if (
        formData.balance_query_quota_per_unit != null &&
        formData.balance_query_quota_per_unit > 0
      ) {
        balanceQuery.quota_per_unit = formData.balance_query_quota_per_unit
      }
    }
    if (mode === 'custom') {
      const method =
        formData.balance_query_method?.trim().toUpperCase() || 'GET'
      const url = formData.balance_query_url?.trim() || ''
      const extract = formData.balance_query_extract?.trim() || ''
      balanceQuery.method = method
      if (url) balanceQuery.url = url
      const headers = parseBalanceQueryHeaders(formData.balance_query_headers)
      if (headers == null) {
        // The schema runs this exact parser, so a null here means the submit
        // bypassed validation; never let a malformed header object be
        // silently dropped from the submitted payload, or the query will run
        // without the credentials the user configured.
        throw new Error(
          'Balance query headers must be a JSON object with string values'
        )
      }
      if (Object.keys(headers).length > 0) {
        balanceQuery.headers = headers
      }
      const body = formData.balance_query_body?.trim() || ''
      if (body) balanceQuery.body = body
      if (extract) balanceQuery.extract = extract
    }
    // Skip writing a bare subscription default: legacy channels never stored
    // balance_query settings, and injecting {"mode":"subscription"} would
    // make every edit look like a settings change to the backend's
    // sensitivity check. The backend treats the default the same way when
    // comparing settings, so both sides agree the default is "not stored".
    const unchanged = BALANCE_QUERY_FORM_FIELDS.every((field) => {
      const value =
        formData[field] ??
        (field === 'balance_query_mode' ? 'subscription' : '')
      return value === (originalBalanceQuery[field] ?? '')
    })
    for (const name of Object.keys(settingsObj)) {
      if (foldBalanceQueryField(name) === 'balance_query') {
        delete settingsObj[name]
      }
    }
    if (
      !unchanged &&
      (mode !== 'subscription' || balanceQueryMembers.length > 0)
    ) {
      settingsObj.balance_query = balanceQuery
    }
    if (!unchanged) balanceQueryMembers = []
  } else {
    for (const name of Object.keys(settingsObj)) {
      if (foldBalanceQueryField(name) === 'balance_query') {
        delete settingsObj[name]
      }
    }
    balanceQueryMembers = []
  }

  const settingsJSON = JSON.stringify(settingsObj)
  if (balanceQueryMembers.length === 0) return settingsJSON
  const separator = Object.keys(settingsObj).length > 0 ? ',' : ''
  return `${settingsJSON.slice(0, -1)}${separator}${balanceQueryMembers.join(',')}}`
}

function normalizeBaseUrl(value: string | undefined): string {
  return String(value || '')
    .trim()
    .replace(/\/+$/, '')
}

/**
 * Transform form data to API payload for creating channel
 */
export function transformFormDataToCreatePayload(formData: ChannelFormValues): {
  mode: 'single' | 'batch' | 'multi_to_single'
  multi_key_mode?: 'random' | 'polling'
  batch_add_set_key_prefix_2_name?: boolean
  channel: Partial<Channel>
} {
  const mode = formData.multi_key_mode || 'single'

  const channel: Partial<Channel> = {
    name: formData.name,
    type: formData.type,
    base_url: normalizeBaseUrl(formData.base_url) || null,
    key: formData.key,
    openai_organization: formData.openai_organization || null,
    models: formData.models,
    group: formatGroups(formData.group),
    model_mapping: formData.model_mapping || null,
    priority: formData.priority || null,
    weight: formData.weight || null,
    test_model: formData.test_model || null,
    auto_ban: formData.auto_ban ?? 1,
    status: formData.status,
    status_code_mapping: formData.status_code_mapping || null,
    tag: formData.tag || null,
    remark: formData.remark || '',
    setting: buildSettingJSON(formData),
    param_override: formData.param_override || null,
    header_override: formData.header_override || null,
    settings: buildSettingsJSON(formData),
    other: formData.other || '',
  }

  // Clean up empty strings to null for optional fields
  Object.keys(channel).forEach((key) => {
    if (channel[key as keyof typeof channel] === '') {
      ;(channel as Record<string, unknown>)[key] = null
    }
  })

  return {
    mode,
    multi_key_mode:
      mode === 'multi_to_single' ? formData.multi_key_type : undefined,
    batch_add_set_key_prefix_2_name:
      mode === 'batch' ? formData.batch_add_set_key_prefix_2_name : undefined,
    channel,
  }
}

/**
 * Transform form data to API payload for updating channel
 */
export function transformFormDataToUpdatePayload(
  formData: ChannelFormValues,
  channelId: number
): Partial<Channel> {
  const payload: Partial<Channel> = {
    id: channelId,
    name: formData.name,
    type: formData.type,
    base_url: normalizeBaseUrl(formData.base_url) || null,
    openai_organization: formData.openai_organization || null,
    models: formData.models,
    group: formatGroups(formData.group),
    model_mapping: formData.model_mapping || null,
    priority: formData.priority ?? 0,
    weight: formData.weight ?? 0,
    test_model: formData.test_model || null,
    auto_ban: formData.auto_ban ?? 1,
    status_code_mapping: formData.status_code_mapping || null,
    tag: formData.tag || null,
    remark: formData.remark || '',
    setting: buildSettingJSON(formData),
    param_override: formData.param_override || null,
    header_override: formData.header_override || null,
    settings: buildSettingsJSON(formData),
    other: formData.other || '',
  }

  // Only include key if it was changed (not empty)
  if (formData.key && formData.key.trim()) {
    payload.key = formData.key
  }

  // Clean up empty strings to null for optional fields
  Object.keys(payload).forEach((key) => {
    if (payload[key as keyof typeof payload] === '') {
      ;(payload as Record<string, unknown>)[key] = null
    }
  })

  // Send explicit empty strings for nullable fields so GORM updates can clear them.
  payload.base_url = normalizeBaseUrl(formData.base_url) || ''
  payload.openai_organization = formData.openai_organization || ''
  payload.test_model = formData.test_model || ''
  payload.tag = formData.tag || ''
  payload.remark = formData.remark || ''
  payload.model_mapping = formData.model_mapping || ''
  payload.status_code_mapping = formData.status_code_mapping || ''
  payload.param_override = formData.param_override || ''
  payload.header_override = formData.header_override || ''

  return payload
}

// ============================================================================
// Validation Helpers
// ============================================================================

/**
 * Validate JSON string
 */
export function validateJSON(value: string): boolean {
  if (!value || value.trim() === '') return true
  try {
    JSON.parse(value)
    return true
  } catch {
    return false
  }
}

/**
 * Validate model mapping format
 */
export function validateModelMapping(value: string): boolean {
  if (!value || value.trim() === '') return true
  return validateJSON(value)
}

/**
 * Parse models string to array
 */
export function parseModels(models: string): string[] {
  if (!models) return []
  return models
    .split(',')
    .map((m) => m.trim())
    .filter((m) => m.length > 0)
}

/**
 * Parse groups string to array
 */
export function parseGroups(groups: string): string[] {
  if (!groups) return []
  return groups
    .split(',')
    .map((g) => g.trim())
    .filter((g) => g.length > 0)
}

/**
 * Format models array to string
 */
export function formatModels(models: string[]): string {
  return models.join(',')
}

/**
 * Format groups array to string
 */
export function formatGroups(groups: string[]): string {
  return groups.join(',')
}

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

const DEGRADATION_SERVER_MESSAGE_MAP: Record<string, string> = {
  'degradation check is disabled': 'Degradation check is disabled',
  'no probes configured': 'No probes configured',
  'channel not found': 'Channel not found',
  'channel_id and model_name are required':
    'Channel ID and Model Name are required',
  'invalid request body': 'Invalid request body',
  'degradation check task enqueued': 'Degradation check task enqueued',
  'target probe task enqueued, results will be written to check logs':
    'Target probe task enqueued, results will be written to check logs',
  'target probe queued successfully': 'Target probe queued successfully',
  'degradation check task already active, request merged into the running round':
    'Degradation check task already active, request merged into the running round',
  'another degradation check task is already running, retry after it finishes':
    'Another degradation check task is already running, retry after it finishes',
  'a single-target degradation check is already running, retry after it finishes':
    'A single-target degradation check is already running, retry after it finishes',
  'degradation check task already active for this target':
    'Degradation check task already active for this target',
}

/**
 * Maps known backend degradation server messages to localized i18n strings.
 * If rawMsg matches a known server response, it returns the localized translation.
 * Otherwise, it falls back to the provided fallback text or the raw message.
 */
export function localizeDegradationMessage(
  rawMsg?: string,
  fallback?: string
): string {
  if (!rawMsg) return fallback || ''
  const trimmed = rawMsg.trim().toLowerCase()
  const translationKey = DEGRADATION_SERVER_MESSAGE_MAP[trimmed]
  if (translationKey) {
    return i18next.t(translationKey)
  }
  if (i18next.exists(rawMsg.trim())) {
    return i18next.t(rawMsg.trim())
  }
  return fallback || rawMsg
}

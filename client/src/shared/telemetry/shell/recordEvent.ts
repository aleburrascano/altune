import { apiFetch } from '@shared/apiClient';
import type { JsonObject } from '@shared/files';

import { getSessionId } from '@shared/telemetry/eventSession';

export type DiscoveryEventType =
  | 'results_shown'
  | 'result_clicked'
  | 'play'
  | 'skip'
  | 'completed'
  | 'library_add'
  | 'wrong_album'
  | 'search_failed'
  | 'search_degraded'
  | 'playback_health'
  | 'detail_health'
  | 'acquisition_ui'
  | 'client_error'
  | 'user_action'
  | 'failure_shown'
  | 'sse_reconnect'
  | 'outbox_flush_failed'
  | 'download_failed'
  | 'screen_view'
  | 'play_start';

export type EventPayload = JsonObject;

export type DiscoveryEvent = {
  type: DiscoveryEventType;
  search_id?: string | undefined;
  event_id?: string | undefined;
  client_occurred_at?: string | undefined;
  payload?: EventPayload;
};

export async function recordEvent(event: DiscoveryEvent): Promise<void> {
  const body: DiscoveryEvent = {
    ...event,
    payload: { ...(event.payload ?? {}), session_id: getSessionId() },
  };
  await apiFetch('/v1/discovery/events', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

import { apiFetch } from '@shared/api-client';
import { isLoopEnabled } from '@shared/killSwitch/killSwitch';

import { getSessionId } from './session';

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
  | 'detail_health';

export type DiscoveryEvent = {
  type: DiscoveryEventType;
  search_id?: string | undefined;
  event_id?: string | undefined;
  client_occurred_at?: string | undefined;
  payload?: Record<string, unknown>;
};

export class TelemetryGatedError extends Error {
  constructor() {
    super('telemetry flush disabled by kill switch');
    this.name = 'TelemetryGatedError';
  }
}

export async function recordEvent(event: DiscoveryEvent): Promise<void> {
  if (!isLoopEnabled('telemetryFlush')) throw new TelemetryGatedError();
  const body: DiscoveryEvent = {
    ...event,
    payload: { ...(event.payload ?? {}), session_id: getSessionId() },
  };
  await apiFetch<void>('/v1/discovery/events', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

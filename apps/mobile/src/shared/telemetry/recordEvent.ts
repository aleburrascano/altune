/**
 * recordEvent — POST /v1/discovery/events, session-stamped.
 *
 * The behavioral-event wire type and its send function. Lives in telemetry, not
 * api-client, so the base HTTP layer never imports up into telemetry: recordEvent
 * calls `apiFetch` (the base) and stamps the rotating `session_id` from `session`
 * here. (Structure audit F1 — removes the api-client ⇄ telemetry dependency
 * cycle; recordEvent was telemetry misfiled in the search client.)
 */

import { apiFetch } from '@shared/api-client';

import { getSessionId } from './session';

// Behavioral interaction events, all routed through the unified /events envelope
// (the legacy /clicks endpoint was folded into this — clicks are now a
// result_clicked event). query_norm is top-level so the no-click coverage signal
// can match it; everything else rides in payload.
export type DiscoveryEventType =
  | 'results_shown'
  | 'result_clicked'
  | 'play'
  | 'skip'
  | 'completed'
  | 'library_add'
  | 'wrong_album';

export type DiscoveryEvent = {
  type: DiscoveryEventType;
  query_norm?: string;
  // The originating search's keystone. Threaded onto every engagement event so
  // the backend can join the impression/click/play funnel back to its search.
  search_id?: string | undefined;
  // Two-tier reliability fields, set only for the label-critical outbox tier
  // (library_add, wrong_album): an idempotency key the server dedups on, and the
  // client's record time (vs the server received_at).
  event_id?: string | undefined;
  client_occurred_at?: string | undefined;
  payload?: Record<string, unknown>;
};

export async function recordEvent(event: DiscoveryEvent): Promise<void> {
  // Stamp the rotating session_id onto every event's payload (no column — it
  // rides in JSONB) so the backend can derive session-arc signals (abandonment,
  // pogo-sticking) without each call site threading it.
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

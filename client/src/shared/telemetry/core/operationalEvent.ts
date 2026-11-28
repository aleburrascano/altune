import type { DiscoveryEvent, EventPayload } from '@shared/telemetry/core/discoveryEvent';

export type OperationalEventType =
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

export type OperationalEvent = {
  type: OperationalEventType;
  event_id?: string | undefined;
  client_occurred_at?: string | undefined;
  correlation_id?: string | undefined;
  payload?: EventPayload;
};

export type ClientEvent = DiscoveryEvent | OperationalEvent;

export type ClientEventType = ClientEvent['type'];

const OPERATIONAL_TYPES: ReadonlySet<ClientEventType> = new Set<OperationalEventType>([
  'playback_health',
  'detail_health',
  'acquisition_ui',
  'client_error',
  'user_action',
  'failure_shown',
  'sse_reconnect',
  'outbox_flush_failed',
  'download_failed',
  'screen_view',
  'play_start',
]);

export function isOperationalEvent(event: ClientEvent): event is OperationalEvent {
  return OPERATIONAL_TYPES.has(event.type);
}

export function withTopLevelCorrelationId(event: OperationalEvent): OperationalEvent {
  if (event.payload === undefined) return event;
  const { correlation_id: fromPayload, ...payload } = event.payload;
  if (typeof fromPayload !== 'string' || fromPayload === '') return event;
  return { ...event, correlation_id: event.correlation_id ?? fromPayload, payload };
}

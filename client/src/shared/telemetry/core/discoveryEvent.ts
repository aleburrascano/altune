import type { JsonObject } from '@shared/files';

export type DiscoveryEventType =
  | 'results_shown'
  | 'result_clicked'
  | 'play'
  | 'skip'
  | 'completed'
  | 'library_add'
  | 'wrong_album'
  | 'search_failed'
  | 'search_degraded';

export type EventPayload = JsonObject;

export type DiscoveryEvent = {
  type: DiscoveryEventType;
  search_id?: string | undefined;
  event_id?: string | undefined;
  client_occurred_at?: string | undefined;
  payload?: EventPayload;
};

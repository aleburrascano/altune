import type { QueryClient } from '@tanstack/react-query';

import {
  parsePlaylistId,
  parseTrackId,
  type PlaylistId,
  type TrackId,
} from '@shared/api-client/ids';

import type { ServerEvent } from './sse-client';
import type { ServerEventType } from './eventTypes';

export type ServerEventHandler = (queryClient: QueryClient, event: ServerEvent) => void;

// A domain module's slice of the router table: exactly the event types it owns.
export type ServerEventHandlers<K extends ServerEventType> = Record<K, ServerEventHandler>;

export function asString(value: unknown): string | null {
  return typeof value === 'string' ? value : null;
}

// An SSE payload is untrusted wire input, so its track id is parsed rather than asserted: an id
// outside the TrackId shape is treated like a missing one instead of throwing out of the handler.
export function asTrackIdOrNull(value: unknown): TrackId | null {
  const raw = asString(value);
  if (raw === null) return null;
  const parsed = parseTrackId(raw);
  return parsed.ok ? parsed.id : null;
}

export function asPlaylistIdOrNull(value: unknown): PlaylistId | null {
  const raw = asString(value);
  if (raw === null) return null;
  const parsed = parsePlaylistId(raw);
  return parsed.ok ? parsed.id : null;
}

export function stringArray(value: unknown): string[] | null {
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : null;
}

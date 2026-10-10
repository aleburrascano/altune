import type { QueryClient } from '@tanstack/react-query';

import {
  parsePlaylistId,
  parseTrackId,
  type PlaylistId,
  type TrackId,
} from '@shared/apiClient/ids';

import type { ServerEvent } from '@shared/events/sseFrames';

export const SERVER_EVENT_TYPES = [
  'resync',
  'track_added_to_library',
  'track_deleted',
  'track_acquisition_started',
  'track_acquisition_progress',
  'track_acquisition_completed',
  'track_acquisition_failed',
  'track_replace_failed',
  'track_added_to_playlist',
  'tracks_added_to_playlist',
  'track_removed_from_playlist',
  'tracks_removed_from_playlist',
  'playlist_created',
  'playlist_deleted',
  'playlist_renamed',
  'playlist_reordered',
] as const;

export type ServerEventType = (typeof SERVER_EVENT_TYPES)[number];

const KNOWN = new Set<string>(SERVER_EVENT_TYPES);

export function isServerEventType(value: string): value is ServerEventType {
  return KNOWN.has(value);
}

export function recordUnhandledEvent(type: string): void {
  console.warn('[sse] unrecognized event type', { type });
}

export type ServerEventHandler = (queryClient: QueryClient, event: ServerEvent) => void;

export type ServerEventHandlers<K extends ServerEventType> = Record<K, ServerEventHandler>;

export function asString(value: unknown): string | null {
  return typeof value === 'string' ? value : null;
}

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

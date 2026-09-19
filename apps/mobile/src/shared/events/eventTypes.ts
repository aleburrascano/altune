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

// Far above the server's ~20-entry vocabulary, so reaching it means `type` has
// turned high-cardinality rather than that the client is a few releases behind.
// The app keeps this set for its whole lifetime, so it needs an end.
export const MAX_TRACKED_UNHANDLED_TYPES = 64;

const _unhandled = new Set<string>();

export function recordUnhandledEvent(type: string): void {
  console.warn('[sse] unrecognized event type', { type });
  if (_unhandled.size >= MAX_TRACKED_UNHANDLED_TYPES) return;
  _unhandled.add(type);
}

export function unhandledEventTypes(): readonly string[] {
  return [..._unhandled];
}

export function _resetUnhandledEventsForTest(): void {
  _unhandled.clear();
}

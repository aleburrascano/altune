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

// The warning is the whole diagnostic: nothing in the app reads a kept tally, so
// keeping one only grows memory for the lifetime of the process.
export function recordUnhandledEvent(type: string): void {
  console.warn('[sse] unrecognized event type', { type });
}

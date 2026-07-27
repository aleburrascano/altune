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
  'track_removed_from_playlist',
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

const _unhandled = new Set<string>();

export function recordUnhandledEvent(type: string): void {
  _unhandled.add(type);
}

export function unhandledEventTypes(): readonly string[] {
  return [..._unhandled];
}

export function _resetUnhandledEventsForTest(): void {
  _unhandled.clear();
}

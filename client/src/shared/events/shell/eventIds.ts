import { parsePlaylistId, parseTrackId, type PlaylistId, type TrackId } from '@shared/apiClient';

import { asString } from '@shared/events/core/serverEventTypes';

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

import { parsePlaylistId, parseTrackId, type PlaylistId, type TrackId } from '@shared/apiClient';

export function asTrackIdOrNull(value: unknown): TrackId | null {
  if (typeof value !== 'string') return null;
  const parsed = parseTrackId(value);
  return parsed.ok ? parsed.id : null;
}

export function asPlaylistIdOrNull(value: unknown): PlaylistId | null {
  if (typeof value !== 'string') return null;
  const parsed = parsePlaylistId(value);
  return parsed.ok ? parsed.id : null;
}

/**
 * Track detail info rows derived from a discovery result's untyped `extras`.
 *
 * Pure + RN-free so it unit-tests without rendering. `extras` is an untyped
 * wire map (Record<string, unknown>); each key is narrowed before use and
 * absent/empty values are omitted (spec AC#3). Key names verified against the
 * deezer / itunes / musicbrainz / soundcloud track adapters: duration_seconds
 * (int seconds), album (str), isrc (str), popularity (float 0..1, deezer only).
 */

export type InfoRow = { key: string; label: string; value: string };

export function formatDuration(totalSeconds: number): string {
  const whole = Math.floor(totalSeconds);
  const minutes = Math.floor(whole / 60);
  const seconds = whole % 60;
  return `${minutes}:${String(seconds).padStart(2, '0')}`;
}

export function trackInfoRows(extras: Record<string, unknown>): InfoRow[] {
  const rows: InfoRow[] = [];

  const duration = extras['duration_seconds'];
  if (typeof duration === 'number' && Number.isFinite(duration) && duration > 0) {
    rows.push({ key: 'duration', label: 'Duration', value: formatDuration(duration) });
  }

  const album = extras['album'];
  if (typeof album === 'string' && album.length > 0) {
    rows.push({ key: 'album', label: 'Album', value: album });
  }

  const isrc = extras['isrc'];
  if (typeof isrc === 'string' && isrc.length > 0) {
    rows.push({ key: 'isrc', label: 'ISRC', value: isrc });
  }

  const popularity = extras['popularity'];
  if (typeof popularity === 'number' && Number.isFinite(popularity)) {
    rows.push({ key: 'popularity', label: 'Popularity', value: `${Math.round(popularity * 100)}%` });
  }

  return rows;
}

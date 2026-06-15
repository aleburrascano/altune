/**
 * Track detail info rows derived from a discovery result's untyped `extras`.
 *
 * Pure + RN-free so it unit-tests without rendering. `extras` is an untyped
 * wire map (Record<string, unknown>); each key is narrowed before use and
 * absent/empty values are omitted (spec AC#3). Key names verified against the
 * deezer / itunes / musicbrainz / soundcloud track adapters: duration_seconds
 * (int seconds), album (str), isrc (str), popularity (float 0..1, deezer only).
 */

import { formatDuration } from '@shared/lib/format';

export { formatDuration };

export type InfoRowKey = 'duration' | 'album' | 'featuring';
export type InfoRow = { key: InfoRowKey; label: string; value: string };

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

  const featured = extras['featured_artists'];
  if (Array.isArray(featured) && featured.length > 0) {
    const names = featured.filter((n): n is string => typeof n === 'string' && n.length > 0);
    if (names.length > 0) {
      rows.push({ key: 'featuring', label: 'Featuring', value: names.join(', ') });
    }
  }

  return rows;
}

const _FEAT_RE = /(?:\(|\[)?\s*(?:feat\.?|ft\.?|featuring|with)\s+([^)\]]+?)(?:\)|\]|$)/i;

export function extractFeaturedFromText(
  title: string,
  subtitle: string | null,
): string | null {
  for (const text of [title, subtitle ?? '']) {
    const match = _FEAT_RE.exec(text);
    if (match?.[1]) {
      return match[1].trim();
    }
  }
  return null;
}

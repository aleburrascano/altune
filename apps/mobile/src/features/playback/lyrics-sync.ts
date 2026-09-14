import type { SyncedLine } from '@shared/api-client/lyrics';

// Returns -1 (no active line) when positionMs is NaN. Lines whose milliseconds is not a
// finite number (the API payload is untrusted) are skipped rather than coerced.
export function activeLineIndex(lines: SyncedLine[], positionMs: number): number {
  let active = -1;
  if (Number.isNaN(positionMs)) return active;
  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i];
    if (line === undefined) break;
    const ms: unknown = line.milliseconds;
    if (typeof ms !== 'number' || !Number.isFinite(ms)) continue;
    if (ms > positionMs) break;
    active = i;
  }
  return active;
}

export type LyricsView = 'loading' | 'error' | 'unavailable' | 'synced' | 'plain';

export function _lyricsView(input: {
  isLoading: boolean;
  isError: boolean;
  plain: string;
  syncedCount: number;
}): LyricsView {
  if (input.isLoading) return 'loading';
  if (input.isError) return 'error';
  if (input.syncedCount > 0) return 'synced';
  if (input.plain.trim().length > 0) return 'plain';
  return 'unavailable';
}

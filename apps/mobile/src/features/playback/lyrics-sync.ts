/**
 * Pure helpers for the lyrics surface — the logic the sheet would otherwise
 * hide inside JSX (same pattern as `state.ts` in discover/library).
 */
import type { SyncedLine } from '@shared/api-client/lyrics';

/** Which synced line is active at `positionMs`.
 *
 * Returns the index of the last line whose start time has passed, or -1 before
 * the first line (intros, instrumental openings). Assumes lines are ordered by
 * `milliseconds`, which is how the provider returns them.
 *
 * Linear scan, not binary: lyric lists are ~60 entries and this runs on a
 * position tick, so the constant factor is irrelevant and the code stays
 * obviously correct at the boundaries. */
export function activeLineIndex(lines: SyncedLine[], positionMs: number): number {
  let active = -1;
  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i];
    if (line === undefined || line.milliseconds > positionMs) break;
    active = i;
  }
  return active;
}

export type LyricsView = 'loading' | 'error' | 'unavailable' | 'synced' | 'plain';

/** The five-state view for the lyrics sheet.
 *
 * `unavailable` is the ordinary case, not a failure: the endpoint answers 200
 * with an empty payload for anything Deezer can't resolve, so an empty body must
 * never render as an error. */
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

import type { SyncedLine } from '@shared/api-client/lyrics';

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

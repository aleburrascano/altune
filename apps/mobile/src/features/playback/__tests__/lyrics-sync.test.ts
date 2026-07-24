import { activeLineIndex, _lyricsView } from '../lyrics-sync';
import type { SyncedLine } from '@shared/api-client/lyrics';

const line = (ms: number, text: string): SyncedLine => ({
  timecode: '',
  line: text,
  milliseconds: ms,
  duration: 0,
});

describe('activeLineIndex', () => {
  const lines = [line(0, 'one'), line(5_000, 'two'), line(10_000, 'three')];

  it('is -1 before the first line starts', () => {
    expect(activeLineIndex([line(2_000, 'intro')], 500)).toBe(-1);
  });

  it('activates a line exactly at its start time', () => {
    expect(activeLineIndex(lines, 5_000)).toBe(1);
  });

  it('holds the last started line until the next one begins', () => {
    expect(activeLineIndex(lines, 9_999)).toBe(1);
    expect(activeLineIndex(lines, 10_000)).toBe(2);
  });

  it('stays on the final line past the end', () => {
    expect(activeLineIndex(lines, 999_000)).toBe(2);
  });

  it('is -1 for an empty list', () => {
    expect(activeLineIndex([], 1_000)).toBe(-1);
  });
});

describe('_lyricsView', () => {
  const base = { isLoading: false, isError: false, plain: '', syncedCount: 0 };

  it('prefers synced lines over plain text', () => {
    expect(_lyricsView({ ...base, plain: 'words', syncedCount: 3 })).toBe('synced');
  });

  it('falls back to plain when there are no synced lines', () => {
    expect(_lyricsView({ ...base, plain: 'words' })).toBe('plain');
  });

  // The endpoint answers 200-with-empty for anything it can't resolve, so an
  // empty payload must read as "no lyrics", never as a failure.
  it('reads an empty payload as unavailable, not an error', () => {
    expect(_lyricsView(base)).toBe('unavailable');
    expect(_lyricsView({ ...base, plain: '   \n  ' })).toBe('unavailable');
  });

  it('reports loading and error ahead of content', () => {
    expect(_lyricsView({ ...base, isLoading: true, syncedCount: 5 })).toBe('loading');
    expect(_lyricsView({ ...base, isError: true, plain: 'stale' })).toBe('error');
  });
});

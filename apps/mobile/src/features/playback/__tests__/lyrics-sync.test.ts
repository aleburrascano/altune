import type { SyncedLine } from '@shared/api-client/lyrics';

import { activeLineIndex, _lyricsView } from '../lyrics-sync';

function line(milliseconds: number): SyncedLine {
  return { timecode: '00:00', line: `line@${String(milliseconds)}`, milliseconds, duration: 500 };
}

describe('activeLineIndex — which synced line is current at a position', () => {
  const lines = [line(0), line(1000), line(2000)];

  it('is -1 before the first line begins', () => {
    expect(activeLineIndex(lines, -1)).toBe(-1);
  });

  it('activates a line exactly at its timestamp', () => {
    expect(activeLineIndex(lines, 1000)).toBe(1);
  });

  it('holds the previous line until the next one begins', () => {
    expect(activeLineIndex(lines, 1999)).toBe(1);
  });

  it('activates the last line once its timestamp has passed', () => {
    expect(activeLineIndex(lines, 5000)).toBe(2);
  });

  it('is -1 for an empty lyric', () => {
    expect(activeLineIndex([], 5000)).toBe(-1);
  });
});

describe('_lyricsView — which view the lyrics sheet shows', () => {
  it('is loading first, ahead of an error and content', () => {
    expect(_lyricsView({ isLoading: true, isError: true, plain: 'words', syncedCount: 3 })).toBe(
      'loading',
    );
  });

  it('is error ahead of any content when not loading', () => {
    expect(_lyricsView({ isLoading: false, isError: true, plain: 'words', syncedCount: 3 })).toBe(
      'error',
    );
  });

  it('is synced when synced lines exist and it is neither loading nor errored', () => {
    expect(_lyricsView({ isLoading: false, isError: false, plain: 'words', syncedCount: 1 })).toBe(
      'synced',
    );
  });

  it('is plain when there are no synced lines but non-blank plain text', () => {
    expect(_lyricsView({ isLoading: false, isError: false, plain: 'words', syncedCount: 0 })).toBe(
      'plain',
    );
  });

  it('is unavailable when the plain text is only whitespace', () => {
    expect(_lyricsView({ isLoading: false, isError: false, plain: '   ', syncedCount: 0 })).toBe(
      'unavailable',
    );
  });

  it('is unavailable when there is nothing at all', () => {
    expect(_lyricsView({ isLoading: false, isError: false, plain: '', syncedCount: 0 })).toBe(
      'unavailable',
    );
  });
});

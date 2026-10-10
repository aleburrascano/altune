import type { LibrarySort } from '@shared/apiClient/library';
import {
  ALBUM_SORT_OPTIONS,
  ARTIST_SORT_OPTIONS,
  PLAYLIST_SORT_OPTIONS,
  TRACK_SORT_OPTIONS,
  librarySection,
} from '@features/library/domain/libraryView';

const WIRE_VALUES: Record<LibrarySort, true> = { recent: true, az: true, year: true };

describe('sort option lists — labels are for display, keys are the wire values the server applies', () => {
  it('albums sort by recent, A–Z or year', () => {
    expect(ALBUM_SORT_OPTIONS).toEqual([
      { key: 'recent', label: 'Recent' },
      { key: 'az', label: 'A–Z' },
      { key: 'year', label: 'Year' },
    ]);
  });

  it('tracks sort by recent, A–Z or year', () => {
    expect(TRACK_SORT_OPTIONS).toEqual([
      { key: 'recent', label: 'Recent' },
      { key: 'az', label: 'A–Z' },
      { key: 'year', label: 'Year' },
    ]);
  });

  it('artists offer no year sort, since an artist group has no year', () => {
    expect(ARTIST_SORT_OPTIONS).toEqual([
      { key: 'recent', label: 'Recent' },
      { key: 'az', label: 'A–Z' },
    ]);
  });

  it('playlists offer no year sort either', () => {
    expect(PLAYLIST_SORT_OPTIONS).toEqual([
      { key: 'recent', label: 'Recent' },
      { key: 'az', label: 'A–Z' },
    ]);
  });

  it('every key across every list is a value the LibrarySort wire contract accepts', () => {
    const all = [
      ...ALBUM_SORT_OPTIONS,
      ...ARTIST_SORT_OPTIONS,
      ...TRACK_SORT_OPTIONS,
      ...PLAYLIST_SORT_OPTIONS,
    ];
    for (const { key } of all) {
      expect(WIRE_VALUES[key]).toBe(true);
    }
  });
});

function inputs(
  over: Partial<{
    isLoading: boolean;
    error: Error | null;
    count: number;
    showEmpty: boolean;
  }> = {},
) {
  return { isLoading: false, error: null as Error | null, count: 1, showEmpty: true, ...over };
}

const viewOf = (over: Parameters<typeof inputs>[0]) => librarySection(inputs(over));

describe('librarySection — precedence', () => {
  it('a non-empty, non-error, settled state is ready', () => {
    expect(viewOf({ count: 2 })).toBe('ready');
  });

  it('an empty settled state that may show empty is empty, not ready', () => {
    expect(viewOf({ count: 0 })).toBe('empty');
  });

  it('an empty settled state that may not show empty stays ready', () => {
    expect(viewOf({ count: 0, showEmpty: false })).toBe('ready');
  });

  it('an error settled state is error, not empty and not ready', () => {
    expect(viewOf({ error: new Error('boom'), count: 0 })).toBe('error');
  });

  it('loading wins over a present error, so the spinner is not pre-empted by a stale error', () => {
    expect(viewOf({ isLoading: true, error: new Error('boom'), count: 0 })).toBe('loading');
  });

  it('error wins over emptiness, so a failed load is not mistaken for an empty library', () => {
    expect(viewOf({ error: new Error('boom'), count: 0, showEmpty: true })).toBe('error');
  });

  it('emptiness needs count 0 and showEmpty once loading is done and there is no error', () => {
    expect(viewOf({ count: 0 })).toBe('empty');
    expect(viewOf({ count: 1 })).toBe('ready');
  });
});

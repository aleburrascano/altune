import type { LibrarySort } from '@shared/api-client/library';

import {
  ALBUM_SORT_OPTIONS,
  ARTIST_SORT_OPTIONS,
  PLAYLIST_SORT_OPTIONS,
  TRACK_SORT_OPTIONS,
} from '../ui/sort';

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

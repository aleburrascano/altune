import { asPlaylistId } from '@shared/api-client/ids';
import type { QueueSource } from '@shared/playback/types';

import { buildUpNextItems, queueSourceLabel } from '../queueItem';
import { libraryTrack } from './fixtures';

describe('queueSourceLabel', () => {
  it.each<[string, QueueSource | null, string]>([
    ['null', null, 'Queue'],
    ['library', { kind: 'library' }, 'Playing from Library'],
    ['search', { kind: 'search', query: 'q' }, 'Playing from search'],
    [
      'playlist',
      { kind: 'playlist', playlistId: asPlaylistId('pl-1'), name: 'Road' },
      'Playing from Road',
    ],
  ])('labels %s source', (_name, source, label) => {
    expect(queueSourceLabel(source)).toBe(label);
  });
});

describe('buildUpNextItems', () => {
  const tracks = [
    libraryTrack({ title: 'A', durationSeconds: 60 }),
    libraryTrack({ title: 'B' }),
    libraryTrack({ title: 'C' }),
  ];

  it('returns nothing for an empty queue', () => {
    expect(buildUpNextItems([], [], -1)).toEqual([]);
  });

  it('returns nothing when the current track is last', () => {
    expect(buildUpNextItems(tracks, [0, 1, 2], 2)).toEqual([]);
  });

  it('follows the shuffled play order after the current index', () => {
    const items = buildUpNextItems(tracks, [2, 0, 1], 0);
    expect(items.map((i) => [i.title, i.trackIndex, i.queueIndex])).toEqual([
      ['A', 0, 1],
      ['B', 1, 2],
    ]);
  });

  it('carries duration and skips play-order entries with no track', () => {
    const items = buildUpNextItems(tracks, [1, 0, 9], 0);
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ title: 'A', durationSeconds: 60, queueIndex: 1 });
  });
});

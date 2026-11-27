import { QueryClient } from '@tanstack/react-query';

import { libraryKeys, playlistKeys } from '@shared/query';
import {
  applyServerEvent,
  EVENT_INVALIDATES,
  SERVER_EVENT_TYPES,
  type ServerEvent,
} from '@shared/events';

import { recordingCache } from '@shared/events/__tests__/recordingCache';

function serverEvent(type: string, data: ServerEvent['data'] = {}): ServerEvent {
  return { id: 'e1', type, data } as ServerEvent;
}

function invalidatedBy(event: ServerEvent): unknown[] {
  const cache = recordingCache(new QueryClient());
  applyServerEvent(cache, event);
  return cache.invalidated;
}

describe('EVENT_INVALIDATES', () => {
  it('has an entry for every server event type', () => {
    expect(Object.keys(EVENT_INVALIDATES).sort()).toEqual([...SERVER_EVENT_TYPES].sort());
  });

  it.each([
    ['track_deleted', { track_id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' }],
    ['playlist_created', { playlist_id: 'p1' }],
    ['playlist_deleted', { playlist_id: 'p1' }],
    ['track_added_to_playlist', { playlist_id: 'p1' }],
    ['tracks_added_to_playlist', { playlist_id: 'p1' }],
    ['track_removed_from_playlist', {}],
    ['tracks_removed_from_playlist', {}],
    ['resync', {}],
  ])('applying %s invalidates exactly the keys the table names', (type, data) => {
    expect(invalidatedBy(serverEvent(type, data))).toEqual(
      EVENT_INVALIDATES[type as keyof typeof EVENT_INVALIDATES],
    );
  });

  it.each([
    'track_acquisition_started',
    'track_acquisition_progress',
    'playlist_renamed',
    'playlist_reordered',
  ])('applying %s patches and invalidates nothing', (type) => {
    expect(EVENT_INVALIDATES[type as keyof typeof EVENT_INVALIDATES]).toEqual([]);
    expect(invalidatedBy(serverEvent(type, {}))).toEqual([]);
  });

  it('refreshes the playlist list and every library-derived cache when a track is deleted', () => {
    expect(EVENT_INVALIDATES.track_deleted).toEqual([
      libraryKeys.albumsPrefix,
      libraryKeys.artistsPrefix,
      libraryKeys.summary,
      libraryKeys.lookupPrefix,
      playlistKeys.list,
    ]);
  });
});

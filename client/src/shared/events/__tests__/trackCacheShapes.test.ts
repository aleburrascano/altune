import { QueryClient } from '@tanstack/react-query';

import { asPlaylistId, asTrackId } from '@shared/apiClient/ids';
import type { ListTracksResponse, TrackResponse } from '@shared/apiClient/trackResponse';
import type { PlaylistDetailResponse } from '@shared/apiClient/playlistResponse';
import { libraryKeys, playlistKeys } from '@shared/query/queryKeys';

import {
  PATCH_POLICY,
  RESIZE_POLICY,
  TRACK_CACHE_FAMILIES,
  TRACK_CACHE_FAMILY_LIST,
  TRACK_SHAPES,
  writeFamily,
  type TrackCacheShape,
} from '@shared/events/trackCacheShapes';

function makeTrack(id: string): TrackResponse {
  return {
    id: asTrackId(id),
    title: id,
    artist: 'A',
    acquisition_status: 'ready',
  } as TrackResponse;
}

function makePage(items: TrackResponse[]): ListTracksResponse {
  return { items, total: items.length, limit: 20, offset: 0, has_more: false };
}

function makePlaylist(tracks: TrackResponse[]): PlaylistDetailResponse {
  return {
    id: asPlaylistId('p1'),
    name: 'P',
    track_count: tracks.length,
    tracks,
  } as PlaylistDetailResponse;
}

function dropAll(): TrackResponse[] {
  return [];
}
function retitle(items: TrackResponse[]): TrackResponse[] {
  return items.map((t) => ({ ...t, title: 'patched' }));
}

describe('TRACK_SHAPES', () => {
  it.each(['paged', 'flat', 'playlistDetails'] as const)(
    'reads no items from undefined %s data',
    (shape) => {
      expect(TRACK_SHAPES[shape].items(undefined)).toEqual([]);
    },
  );

  it('every family names a shape that has an accessor', () => {
    const families = Object.values(TRACK_CACHE_FAMILIES);
    expect(families.length).toBeGreaterThan(0);
    expect(TRACK_CACHE_FAMILY_LIST).toEqual(families);
    for (const family of families) {
      const shape: TrackCacheShape = family.shape;
      expect(TRACK_SHAPES[shape].items).toBeInstanceOf(Function);
      expect(TRACK_SHAPES[shape].write).toBeInstanceOf(Function);
    }
  });
});

describe('RESIZE_POLICY', () => {
  it('removing the only track of a one-page paged cache leaves total 0', () => {
    const prev = { pages: [makePage([makeTrack('a')])], pageParams: [0] };
    const next = TRACK_SHAPES.paged.write(prev, dropAll, RESIZE_POLICY) as typeof prev;
    expect(next.pages[0]?.items).toEqual([]);
    expect(next.pages[0]?.total).toBe(0);
  });

  it('removing the only track of a playlist leaves track_count 0', () => {
    const next = TRACK_SHAPES.playlistDetails.write(
      makePlaylist([makeTrack('a')]),
      dropAll,
      RESIZE_POLICY,
    ) as PlaylistDetailResponse;
    expect(next.track_count).toBe(0);
  });
});

describe('PATCH_POLICY', () => {
  it('keeps total and track_count unchanged', () => {
    const list = TRACK_SHAPES.flat.write(
      makePage([makeTrack('a')]),
      retitle,
      PATCH_POLICY,
    ) as ListTracksResponse;
    expect(list.total).toBe(1);
    expect(list.items[0]?.title).toBe('patched');

    const playlist = TRACK_SHAPES.playlistDetails.write(
      makePlaylist([makeTrack('a')]),
      retitle,
      PATCH_POLICY,
    ) as PlaylistDetailResponse;
    expect(playlist.track_count).toBe(1);
    expect(playlist.tracks[0]?.title).toBe('patched');
  });
});

describe('writeFamily', () => {
  it('leaves a look-alike query outside the family holding the same reference', () => {
    const client = new QueryClient();
    const albums = makePage([makeTrack('a')]);
    client.setQueryData([...libraryKeys.albumsPrefix, 'x'], albums);
    client.setQueryData(playlistKeys.detail(asPlaylistId('p1')), makePlaylist([makeTrack('a')]));

    const family = TRACK_CACHE_FAMILIES.lookup;
    writeFamily(client, family, { queryKey: family.prefix }, dropAll, RESIZE_POLICY);

    expect(client.getQueryData([...libraryKeys.albumsPrefix, 'x'])).toBe(albums);
  });
});

import { asPlaylistId, asTrackId } from '@shared/api-client/ids';
import type { PlaylistDetailResponse } from '@shared/api-client/playlistResponse';
import { libraryKeys, playlistKeys } from '@shared/query/queryKeys';

import { removeTrackFromCaches } from '@shared/events/trackCachePatch';
import { captureTrackPlacements, restoreTrackPlacements } from '@shared/events/trackPlacements';
import {
  makeInfinite,
  makePage,
  makePlaylistDetail,
  makeTrack,
  newClient,
  registerUnfetchedQuery,
  seedTracksPrefix,
} from '@shared/events/__tests__/trackCacheFixtures';

describe('captureTrackPlacements + restoreTrackPlacements — undo an optimistic removal', () => {
  it('round-trips a removal across every family back to the exact prior data', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    const pages = [
      makePage([makeTrack({ id: asTrackId('a') })], { total: 5 }),
      makePage([makeTrack({ id: asTrackId('b') }), target], { total: 8 }),
    ];
    seedTracksPrefix(client, pages);
    const lookup = makePage([target, makeTrack({ id: asTrackId('c') })], { total: 9 });
    client.setQueryData(libraryKeys.lookup('q'), lookup);
    const detail = makePlaylistDetail('p1', [target, makeTrack({ id: asTrackId('d') }), target]);
    client.setQueryData(playlistKeys.detail(asPlaylistId('p1')), detail);

    const placements = captureTrackPlacements(client, asTrackId('target'));
    removeTrackFromCaches(client, asTrackId('target'));
    restoreTrackPlacements(client, placements);

    expect(client.getQueryData(libraryKeys.tracks('q', 'sort'))).toEqual(makeInfinite(pages));
    expect(client.getQueryData(libraryKeys.lookup('q'))).toEqual(lookup);
    expect(client.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toEqual(detail);
  });

  it('keeps a removal made by another mutation in the meantime', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    const other = makeTrack({ id: asTrackId('other') });
    client.setQueryData(libraryKeys.featuring('who'), makePage([other, target], { total: 2 }));

    const placements = captureTrackPlacements(client, asTrackId('target'));
    removeTrackFromCaches(client, asTrackId('target'));
    removeTrackFromCaches(client, asTrackId('other'));
    restoreTrackPlacements(client, placements);

    expect(client.getQueryData(libraryKeys.featuring('who'))).toEqual(
      makePage([target], { total: 1 }),
    );
  });

  it('leaves an entry alone when the track is already back, so it never duplicates', () => {
    const client = newClient();
    const target = makeTrack({ id: asTrackId('target') });
    seedTracksPrefix(client, [makePage([target], { total: 1 })]);
    client.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makePlaylistDetail('p1', [target]),
    );

    const placements = captureTrackPlacements(client, asTrackId('target'));
    restoreTrackPlacements(client, placements);

    expect(client.getQueryData(libraryKeys.tracks('q', 'sort'))).toEqual(
      makeInfinite([makePage([target], { total: 1 })]),
    );
    expect(
      client.getQueryData<PlaylistDetailResponse>(playlistKeys.detail(asPlaylistId('p1')))!.tracks,
    ).toEqual([target]);
  });

  it('captures nothing for an absent track and skips entries evicted before restore', () => {
    const client = newClient();
    expect(captureTrackPlacements(client, asTrackId('missing'))).toEqual([]);

    const target = makeTrack({ id: asTrackId('target') });
    client.setQueryData(libraryKeys.lookup('q'), makePage([target]));
    registerUnfetchedQuery(client, libraryKeys.featuring('none'));
    const placements = captureTrackPlacements(client, asTrackId('target'));
    client.removeQueries({ queryKey: libraryKeys.lookup('q') });
    restoreTrackPlacements(client, placements);

    expect(client.getQueryData(libraryKeys.lookup('q'))).toBeUndefined();
  });
});

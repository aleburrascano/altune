import { useMemo } from 'react';

import { detailHref } from '@shared/lib/detail-handoff';
import type { Navigator } from '@shared/navigation';
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { PlaylistResponse, TrackResponse } from '@shared/api-client/types';

import type { AlbumGroup, ArtistGroup } from '@shared/api-client/library';
import { albumToDiscoveryResult, artistToDiscoveryResult } from '../library-to-discovery';

const toDetail = (item: DiscoveryResult) => detailHref('/library/detail', item);

function buildNavigation(navigator: Navigator) {
  return {
    navigateToTrack: (t: TrackResponse) => navigator.push(toDetail(trackToDiscoveryResult(t))),
    navigateToAlbum: (a: AlbumGroup) => navigator.push(toDetail(albumToDiscoveryResult(a))),
    navigateToArtist: (a: ArtistGroup) => navigator.push(toDetail(artistToDiscoveryResult(a))),
    navigateToPlaylist: (p: PlaylistResponse) => navigator.push(`/library/playlist/${p.id}`),
  };
}

export function useLibraryNavigation(navigator: Navigator) {
  return useMemo(() => buildNavigation(navigator), [navigator]);
}

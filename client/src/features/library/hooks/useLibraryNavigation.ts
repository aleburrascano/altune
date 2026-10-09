import { useMemo } from 'react';
import { detailHref } from '@shared/navigation/detailHandoff';
import type { Navigator } from '@shared/navigation';
import { trackToDiscoveryResult } from '@shared/api-client/trackToDiscoveryResult';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { PlaylistResponse } from '@shared/api-client/playlistResponse';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import type { AlbumGroup, ArtistGroup } from '@shared/api-client/library';
import {
  albumToDiscoveryResult,
  artistToDiscoveryResult,
} from '@features/library/domain/libraryToDiscovery';

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

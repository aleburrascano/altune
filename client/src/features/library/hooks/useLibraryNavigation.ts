import { useMemo } from 'react';
import { detailHref } from '@shared/navigation';
import type { Navigator } from '@shared/navigation';
import { trackToDiscoveryResult } from '@shared/catalog';
import type { AlbumGroup, ArtistGroup } from '@shared/apiClient';
import type { DiscoveryResult, PlaylistResponse, TrackResponse } from '@shared/catalog';
import {
  albumToDiscoveryResult,
  artistToDiscoveryResult,
} from '@features/library/domain/libraryToDiscovery';

function toDetail(item: DiscoveryResult) {
  return detailHref('/library/detail', item);
}

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

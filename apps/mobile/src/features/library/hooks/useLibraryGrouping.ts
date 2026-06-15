import { useMemo } from 'react';

import type { TrackResponse } from '@shared/api-client/types';
import { deriveAlbums, deriveArtists } from '@shared/lib/derive-library-groups';

export type { AlbumGroup, ArtistGroup } from '@shared/lib/derive-library-groups';
export { deriveAlbums, deriveArtists } from '@shared/lib/derive-library-groups';

export function useLibraryGrouping(tracks: TrackResponse[]) {
  const albums = useMemo(() => deriveAlbums(tracks), [tracks]);
  const artists = useMemo(() => deriveArtists(tracks), [tracks]);
  return { albums, artists };
}

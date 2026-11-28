import type {
  AlbumGroup as WireAlbumGroup,
  ArtistGroup as WireArtistGroup,
  ListAlbumsResponse,
  ListArtistsResponse,
} from '@shared/apiClient';

import type {
  AlbumGroup,
  AlbumsPage,
  ArtistGroup,
  ArtistsPage,
} from '@features/library/core/libraryGroups';

function toAlbumGroup(wire: WireAlbumGroup): AlbumGroup {
  return {
    key: wire.key,
    album: wire.album,
    artist: wire.artist,
    artwork_url: wire.artwork_url,
    year: wire.year,
    track_count: wire.track_count,
    most_recent_added_at: wire.most_recent_added_at,
  };
}

function toArtistGroup(wire: WireArtistGroup): ArtistGroup {
  return {
    key: wire.key,
    artist: wire.artist,
    artwork_url: wire.artwork_url,
    track_count: wire.track_count,
    most_recent_added_at: wire.most_recent_added_at,
  };
}

export function toAlbumsPage(wire: ListAlbumsResponse): AlbumsPage {
  return { items: wire.items.map(toAlbumGroup), total: wire.total };
}

export function toArtistsPage(wire: ListArtistsResponse): ArtistsPage {
  return { items: wire.items.map(toArtistGroup), total: wire.total };
}

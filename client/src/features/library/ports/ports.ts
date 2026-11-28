import type {
  DiscoveryKind,
  DiscoverySearchResponse,
  FeaturedArtist,
  ListPlaylistsResponse,
  ListTracksResponse,
  PlaylistDetailResponse,
  PlaylistId,
  TrackId,
  TrackResponse,
} from '@shared/catalog';
import type { confirmDestructive } from '@shared/ui';
import type { LibraryWarn, ShowFailure } from '@features/library/core/errors';
import type {
  AlbumsPage,
  ArtistsPage,
  LibraryQuery,
  LibrarySort,
} from '@features/library/core/libraryGroups';

interface TracksPageParams {
  limit: number;
  offset: number;
  q?: string;
  sort?: LibrarySort;
}

interface AllTracksParams {
  q?: string;
  sort?: LibrarySort;
  signal?: AbortSignal;
}

interface AllTracks {
  items: TrackResponse[];
  truncated: boolean;
}

interface PlaylistsPageParams {
  limit?: number;
  offset?: number;
}

interface DiscoverySearchParams {
  q: string;
  kinds?: DiscoveryKind[];
  limit?: number;
  offset?: number;
  saveHistory?: boolean;
  searchId?: string;
}

export interface LibraryPort {
  warn: LibraryWarn;
  showFailure: ShowFailure;
  confirmDestructive: typeof confirmDestructive;
  getTracks: (params: TracksPageParams, signal?: AbortSignal) => Promise<ListTracksResponse>;
  getAllTracks: (params: AllTracksParams) => Promise<AllTracks>;
  listTracksFeaturing: (
    artist: FeaturedArtist,
    signal?: AbortSignal,
  ) => Promise<Pick<ListTracksResponse, 'items' | 'total'>>;
  deleteTrack: (trackId: TrackId) => Promise<void>;
  reacquireTrack: (trackId: TrackId) => Promise<void>;
  getLibraryAlbums: (query?: LibraryQuery, signal?: AbortSignal) => Promise<AlbumsPage>;
  getLibraryArtists: (query?: LibraryQuery, signal?: AbortSignal) => Promise<ArtistsPage>;
  getPlaylists: (
    page?: PlaylistsPageParams,
    signal?: AbortSignal,
  ) => Promise<ListPlaylistsResponse>;
  getPlaylist: (id: PlaylistId, signal?: AbortSignal) => Promise<PlaylistDetailResponse>;
  searchDiscovery: (
    params: DiscoverySearchParams,
    signal?: AbortSignal,
  ) => Promise<DiscoverySearchResponse>;
}

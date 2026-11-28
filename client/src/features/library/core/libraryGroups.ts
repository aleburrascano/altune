export type LibrarySort = 'recent' | 'az' | 'year';

export interface LibraryQuery {
  q?: string;
  sort?: LibrarySort;
  limit?: number;
  offset?: number;
}

interface LibraryGroup {
  key: string;
  artwork_url: string | null;
  track_count: number;
  most_recent_added_at: string;
}

export interface ArtistGroup extends LibraryGroup {
  artist: string;
}

export interface AlbumGroup extends ArtistGroup {
  album: string;
  year: number | null;
}

interface GroupsPage<Group> {
  items: Group[];
  total: number;
}

export type AlbumsPage = GroupsPage<AlbumGroup>;

export type ArtistsPage = GroupsPage<ArtistGroup>;

export type LibrarySort = 'recent' | 'az' | 'year';

export type LibraryQuery = {
  q?: string;
  sort?: LibrarySort;
  limit?: number;
  offset?: number;
};

export type AlbumGroup = {
  key: string;
  album: string;
  artist: string;
  artwork_url: string | null;
  year: number | null;
  track_count: number;
  most_recent_added_at: string;
};

export type ArtistGroup = {
  key: string;
  artist: string;
  artwork_url: string | null;
  track_count: number;
  most_recent_added_at: string;
};

export type AlbumsPage = {
  items: AlbumGroup[];
  total: number;
};

export type ArtistsPage = {
  items: ArtistGroup[];
  total: number;
};

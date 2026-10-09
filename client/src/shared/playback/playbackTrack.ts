import type { TrackId } from '@shared/api-client/ids';
import type { FeaturedArtist } from '@shared/api-client/trackResponse';

export type PlaybackSource =
  | { readonly kind: 'library'; readonly trackId: TrackId }
  | { readonly kind: 'preview'; readonly previewUrl: string };

export interface PlaybackTrack {
  readonly source: PlaybackSource;
  readonly title: string;
  readonly artist: string;
  readonly artworkUrl: string | null;
  readonly durationSeconds?: number | undefined;
  readonly featuredArtists?: readonly FeaturedArtist[] | undefined;
  readonly searchId?: string | undefined;
  readonly resultSignature?: string | undefined;
}

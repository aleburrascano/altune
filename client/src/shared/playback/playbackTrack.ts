import { asTrackId, type TrackId } from '@shared/api-client/ids';
import type { QueueStateCurrentTrack } from '@shared/api-client/playback';
import type { FeaturedArtist, TrackResponse } from '@shared/api-client/trackResponse';

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

declare const trackKeyBrand: unique symbol;
export type TrackKey = string & { readonly [trackKeyBrand]: true };

export function trackKey(track: PlaybackTrack): TrackKey {
  return (
    track.source.kind === 'library'
      ? `library:${track.source.trackId}`
      : `preview:${track.source.previewUrl}`
  ) as TrackKey;
}

export function toPlaybackTrack(t: TrackResponse): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: t.id },
    title: t.title,
    artist: t.artist,
    artworkUrl: t.artwork_url ?? null,
    durationSeconds: t.duration_seconds ?? undefined,
    featuredArtists: t.featured_artists,
  };
}

export function currentTrackToPlaybackTrack(t: QueueStateCurrentTrack): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: asTrackId(t.id) },
    title: t.title,
    artist: t.artist,
    artworkUrl: t.artwork_url ?? null,
    durationSeconds: t.duration_seconds ?? undefined,
  };
}

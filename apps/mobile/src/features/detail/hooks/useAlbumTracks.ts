/**
 * useAlbumTracks — fetch tracks from an album by provider + external ID.
 *
 * AC#14: Fetches from the first source in the album's sources array.
 * Cached per (provider, external_id) for the session.
 *
 * When a MusicBrainz source is available (different from the primary),
 * fetches MB tracks in parallel and merges featured_artists into the
 * primary tracks by title match — MB carries structured artist-credit
 * data that Deezer/iTunes strip from their responses.
 */

import { useQuery } from '@tanstack/react-query';

import { getAlbumTracks } from '@shared/api-client/enrichment';
import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';

type UseAlbumTracksParams = {
  provider: string;
  externalId: string;
  albumTitle?: string;
  albumArtist?: string | undefined;
  allSources?: DiscoverySource[];
  enabled?: boolean;
};

type UseAlbumTracksReturn = {
  tracks: DiscoveryResult[];
  isLoading: boolean;
  isError: boolean;
  refetch: () => void;
};

function _normTitle(t: string): string {
  return t.replace(/[\(\[\{][^\)\]\}]*[\)\]\}]/g, ' ').toLowerCase().replace(/\s+/g, ' ').trim();
}

function _mergeFeaturing(
  primary: DiscoveryResult[],
  mbTracks: DiscoveryResult[],
): DiscoveryResult[] {
  if (mbTracks.length === 0) return primary;
  const mbByTitle = new Map<string, DiscoveryResult>();
  for (const t of mbTracks) {
    mbByTitle.set(_normTitle(t.title), t);
  }
  return primary.map((track) => {
    if (
      Array.isArray(track.extras['featured_artists']) &&
      (track.extras['featured_artists'] as unknown[]).length > 0
    ) {
      return track;
    }
    const mbMatch = mbByTitle.get(_normTitle(track.title));
    const feat = mbMatch?.extras['featured_artists'];
    if (Array.isArray(feat) && (feat as unknown[]).length > 0) {
      return { ...track, extras: { ...track.extras, featured_artists: feat } };
    }
    return track;
  });
}

export function useAlbumTracks({
  provider,
  externalId,
  albumTitle,
  albumArtist,
  allSources,
  enabled = true,
}: UseAlbumTracksParams): UseAlbumTracksReturn {
  const { data: primaryData, isLoading, isError: isQueryError, refetch } = useQuery({
    queryKey: ['album-tracks', provider, externalId],
    queryFn: () => getAlbumTracks(provider, externalId, undefined, albumTitle, albumArtist),
    enabled,
    staleTime: 1000 * 60 * 30,
  });

  const mbSource = allSources?.find(
    (s) => s.provider === 'musicbrainz' && !(s.provider === provider && s.external_id === externalId),
  );

  const { data: mbQueryData } = useQuery({
    queryKey: ['album-tracks', 'musicbrainz', mbSource?.external_id ?? ''],
    queryFn: () => getAlbumTracks('musicbrainz', mbSource!.external_id),
    enabled: enabled && mbSource !== undefined,
    staleTime: 1000 * 60 * 30,
  });

  const primaryTracks = primaryData?.items ?? [];
  const mbTracks = mbQueryData?.items ?? [];

  const tracks = _mergeFeaturing(primaryTracks, mbTracks);

  return {
    tracks,
    isLoading,
    isError: isQueryError || primaryData?.status === 'error',
    refetch,
  };
}

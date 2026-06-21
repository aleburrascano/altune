/**
 * useRelatedTracks — fetch a track's "Related on SoundCloud" recommendation set.
 *
 * Gated to SoundCloud-sourced tracks: the `/tracks/{id}/related` endpoint is
 * keyed by a SoundCloud numeric track id, which only a result carrying a
 * SoundCloud source has. A result with no SoundCloud source disables the query
 * entirely (no fetch, empty rail). A non-ok payload is treated as a failure and
 * surfaces no items (the rail hides — spec AC#6/AC#7).
 */

import { useQuery } from '@tanstack/react-query';

import {
  getRelatedTracks,
  type DiscoveryResult,
  type DiscoverySource,
} from '@shared/api-client/discovery';

type UseRelatedTracksParams = {
  sources: DiscoverySource[];
  enabled?: boolean;
};

type UseRelatedTracksReturn = {
  relatedTracks: DiscoveryResult[];
  isLoading: boolean;
  isError: boolean;
};

export function useRelatedTracks({
  sources,
  enabled = true,
}: UseRelatedTracksParams): UseRelatedTracksReturn {
  const scSource = sources.find((s) => s.provider === 'soundcloud') ?? null;

  const { data, isLoading, isError } = useQuery({
    queryKey: ['related-tracks', scSource?.external_id ?? ''],
    queryFn: () => getRelatedTracks('soundcloud', scSource!.external_id, 20),
    enabled: enabled && scSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  return {
    relatedTracks: data?.status === 'ok' ? data.items : [],
    isLoading,
    isError: isError || (data !== undefined && data.status !== 'ok'),
  };
}

/**
 * useArtistContent — fetch top tracks and albums for an artist.
 *
 * AC#16-18: Fans out to all providers in the artist's sources array for
 * albums (multi-provider merge), and uses the best sources for top tracks.
 * Albums are deduped by normalized title (keep highest track_count), then
 * sorted newest-first.
 *
 * The hook is a thin composition of two cohesive halves — `useArtistTopTracks`
 * and `useArtistAlbums` — each owning one concern's provider fan-out, merge,
 * and all-failed verdict. The public return contract is their union.
 */

import { useQuery } from '@tanstack/react-query';

import {
  getArtistAlbums,
  getArtistTopTracks,
  type DiscoveryResult,
  type DiscoverySource,
} from '@shared/api-client/discovery';
import {
  backfillAlbumArt,
  dedupAlbumsByTitle,
  dedupeTracksByTitle,
  sortByReleaseDateDesc,
} from '../helpers/artist-content';

type UseArtistContentParams = {
  sources: DiscoverySource[];
  /** Artist name — passed to the backend for MB cross-reference validation. */
  artistName?: string;
  /** Resolved MusicBrainz id — enables identity-safe Last.fm top-tracks. */
  mbid?: string;
  enabled?: boolean;
};

type ArtistTopTracksResult = {
  topTracks: DiscoveryResult[];
  isLoadingTracks: boolean;
  isErrorTracks: boolean;
  refetchTracks: () => void;
};

type ArtistAlbumsResult = {
  albums: DiscoveryResult[];
  isLoadingAlbums: boolean;
  isErrorAlbums: boolean;
  refetchAlbums: () => void;
};

type UseArtistContentReturn = ArtistTopTracksResult & ArtistAlbumsResult;

/**
 * Top tracks — multi-provider, equal sources: Deezer (mainstream) + SoundCloud
 * (underground), merged by title with Deezer precedence and capped at 5. Both
 * key by numeric id; Last.fm is keyed by MBID (identity-safe) so it never falls
 * back to ambiguous name matching, adding the scrobble-popular layer.
 */
export function useArtistTopTracks({
  sources,
  mbid,
  enabled = true,
}: Pick<UseArtistContentParams, 'sources' | 'mbid' | 'enabled'>): ArtistTopTracksResult {
  const deezerSource = sources.find((s) => s.provider === 'deezer') ?? null;
  const scSource = sources.find((s) => s.provider === 'soundcloud') ?? null;

  const {
    data: dzTracksData,
    isLoading: isLoadingDzTracks,
    isError: isErrorDzTracks,
    refetch: refetchDzTracks,
  } = useQuery({
    queryKey: ['artist-top-tracks-dz', deezerSource?.external_id ?? ''],
    queryFn: () => getArtistTopTracks('deezer', deezerSource!.external_id, 5),
    enabled: enabled && deezerSource !== null,
    staleTime: 1000 * 60 * 30,
  });
  const {
    data: scTracksData,
    isLoading: isLoadingScTracks,
    isError: isErrorScTracks,
    refetch: refetchScTracks,
  } = useQuery({
    queryKey: ['artist-top-tracks-sc', scSource?.external_id ?? ''],
    queryFn: () => getArtistTopTracks('soundcloud', scSource!.external_id, 5),
    enabled: enabled && scSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  // Last.fm top-tracks, keyed by MBID (identity-safe) — only when an MBID is
  // known, so it never falls back to ambiguous name matching. No Last.fm
  // *source* is required.
  const {
    data: lfmTracksData,
    isLoading: isLoadingLfmTracks,
    isError: isErrorLfmTracks,
    refetch: refetchLfmTracks,
  } = useQuery({
    queryKey: ['artist-top-tracks-lfm', mbid ?? ''],
    queryFn: () => getArtistTopTracks('lastfm', mbid!, 5),
    enabled: enabled && Boolean(mbid),
    staleTime: 1000 * 60 * 30,
  });

  const topTrackProviders = [
    { source: deezerSource, data: dzTracksData, isLoading: isLoadingDzTracks, isError: isErrorDzTracks, refetch: refetchDzTracks },
    { source: scSource, data: scTracksData, isLoading: isLoadingScTracks, isError: isErrorScTracks, refetch: refetchScTracks },
    { source: mbid ? { provider: 'lastfm', external_id: mbid, url: '' } : null, data: lfmTracksData, isLoading: isLoadingLfmTracks, isError: isErrorLfmTracks, refetch: refetchLfmTracks },
  ].filter((p) => p.source !== null);

  const mergedTopTracks = dedupeTracksByTitle(
    topTrackProviders.flatMap((p) => (p.data?.status === 'ok' ? p.data.items : [])),
  ).slice(0, 5);

  const isLoadingTracks = topTrackProviders.some((p) => p.isLoading);
  const trackOutcomes = topTrackProviders.map(
    (p) => p.isError || (p.data !== undefined && p.data.status !== 'ok'),
  );
  const isErrorTracks = trackOutcomes.length > 0 && trackOutcomes.every(Boolean);

  return {
    topTracks: mergedTopTracks,
    isLoadingTracks,
    isErrorTracks,
    refetchTracks: () => { topTrackProviders.forEach((p) => p.refetch()); },
  };
}

/**
 * Albums — multi-provider union (Deezer + SoundCloud + iTunes), deduped by
 * normalized title (keep highest track_count). `artistName` is threaded to every
 * provider so the backend runs the same MB-spine consensus validation; without
 * it SoundCloud/Deezer bypass validation and leak same-name contamination. When
 * the backend validated (artistName present) we trust its confirmed-first
 * ordering; otherwise we sort by release date.
 */
export function useArtistAlbums({
  sources,
  artistName,
  enabled = true,
}: Pick<UseArtistContentParams, 'sources' | 'artistName' | 'enabled'>): ArtistAlbumsResult {
  const deezerSource = sources.find((s) => s.provider === 'deezer') ?? null;
  const scSource = sources.find((s) => s.provider === 'soundcloud') ?? null;
  const itunesSource = sources.find((s) => s.provider === 'itunes') ?? null;

  const {
    data: dzData,
    isLoading: isLoadingDz,
    isError: isErrorDz,
    refetch: refetchDz,
  } = useQuery({
    queryKey: ['artist-albums-dz', deezerSource?.external_id ?? '', artistName ?? ''],
    queryFn: () => getArtistAlbums('deezer', deezerSource!.external_id, 100, artistName),
    enabled: enabled && deezerSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  // SoundCloud albums also pass artistName so the backend runs the same MB-spine
  // consensus it does for Deezer/iTunes — without it SoundCloud albums bypassed
  // validation and leaked same-name contamination into the merged list.
  const {
    data: scData,
    isLoading: isLoadingSc,
    isError: isErrorSc,
    refetch: refetchSc,
  } = useQuery({
    queryKey: ['artist-albums-sc', scSource?.external_id ?? '', artistName ?? ''],
    queryFn: () => getArtistAlbums('soundcloud', scSource!.external_id, 100, artistName),
    enabled: enabled && scSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  // iTunes is a second mainstream discography source alongside Deezer
  // (docs/providers/itunes.md cap 5). artistName is passed through so the
  // backend applies the same MB consensus validation it does for Deezer.
  const {
    data: itData,
    isLoading: isLoadingIt,
    isError: isErrorIt,
    refetch: refetchIt,
  } = useQuery({
    queryKey: ['artist-albums-it', itunesSource?.external_id ?? '', artistName ?? ''],
    queryFn: () => getArtistAlbums('itunes', itunesSource!.external_id, 100, artistName),
    enabled: enabled && itunesSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  // One descriptor per discography provider. The useQuery calls above stay
  // explicit (rules of hooks forbid looping them), but every downstream
  // aggregation — merge, loading, the all-failed verdict, refetch — derives
  // from this list. Adding a provider is one query block plus one entry here,
  // not edits scattered across five derivations. Order (deezer, soundcloud,
  // itunes) is preserved so the merge keeps its existing precedence.
  const albumProviders = [
    { source: deezerSource, data: dzData, isLoading: isLoadingDz, isError: isErrorDz, refetch: refetchDz },
    { source: scSource, data: scData, isLoading: isLoadingSc, isError: isErrorSc, refetch: refetchSc },
    { source: itunesSource, data: itData, isLoading: isLoadingIt, isError: isErrorIt, refetch: refetchIt },
  ].filter((p) => p.source !== null);

  const mergedAlbums = dedupAlbumsByTitle(
    albumProviders.flatMap((p) => (p.data?.status === 'ok' ? p.data.items : [])),
  );

  // Back-fill artwork for albums with no image (e.g. SoundCloud sets)
  // from a title-matched album from another provider.
  const albumsWithArt = backfillAlbumArt(mergedAlbums);

  const isLoadingAlbums = albumProviders.some((p) => p.isLoading);
  const albumOutcomes = albumProviders.map(
    (p) => p.isError || (p.data !== undefined && p.data.status !== 'ok'),
  );
  const isErrorAlbums = albumOutcomes.length > 0 && albumOutcomes.every(Boolean);

  // The client unions albums across providers, so it owns final ordering: always
  // sort newest-first by release date. The backend normalizes a numeric year onto
  // every album (derived from release_date), so the sort key is present whichever
  // provider supplied the album — MB-validated lists are no longer left unsorted.
  const finalAlbums = sortByReleaseDateDesc(albumsWithArt);

  return {
    albums: finalAlbums,
    isLoadingAlbums,
    isErrorAlbums,
    refetchAlbums: () => { albumProviders.forEach((p) => p.refetch()); },
  };
}

export function useArtistContent(params: UseArtistContentParams): UseArtistContentReturn {
  return {
    ...useArtistTopTracks(params),
    ...useArtistAlbums(params),
  };
}

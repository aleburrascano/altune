import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { FeaturedArtist } from '@shared/apiClient/trackResponse';
import { libraryKeys } from '@shared/query/queryKeys';
import { useLibraryPort } from '@features/library/ports/LibraryPortProvider';
import {
  failureLogFields,
  libraryFailureAlert,
  classifyLibraryError,
} from '@features/library/domain/libraryFailure';
import { showFailureAlert } from '@shared/ui';
import type { DiscoveryResult } from '@shared/apiClient/discovery';
import { detailHref } from '@shared/navigation/detailHandoff';
import { useNavigator, type Navigator } from '@shared/navigation';
import type { LibraryPort } from '@features/library/ports/libraryPort';
import type { FeaturingDetailRoute } from '@features/library/domain/libraryRoutes';

const DIGITS = /^[0-9]+$/;

export function parseDeezerIdParam(raw: string | undefined): number | null {
  if (raw === undefined || !DIGITS.test(raw)) return null;
  const id = Number(raw);
  return Number.isSafeInteger(id) ? id : null;
}

export function parseStringParam(raw: string | string[] | undefined): string | null {
  const first = Array.isArray(raw) ? raw[0] : raw;
  return first === undefined || first.length === 0 ? null : first;
}

function useLoggedFeaturingQueryFailure(error: Error | null, key: string): void {
  useEffect(() => {
    if (error === null) {
      return;
    }
    console.warn('[library] featuring query failed', { key, ...failureLogFields(error) });
  }, [error, key]);
}

export function useTracksFeaturing(input: FeaturedArtist) {
  const port = useLibraryPort();
  const nonFiniteId = input.deezer_id != null && !Number.isFinite(input.deezer_id);
  const fa: FeaturedArtist = nonFiniteId ? { ...input, deezer_id: null } : input;
  const key = fa.mbid ?? (fa.deezer_id != null ? `dz:${fa.deezer_id}` : `name:${fa.name}`);
  const { data, error, isLoading, isError, isRefetching, refetch } = useQuery({
    queryKey: libraryKeys.featuring(key),
    queryFn: () => port.listTracksFeaturing(fa),
    enabled: fa.name.length > 0 || fa.mbid != null || fa.deezer_id != null,
    staleTime: 60_000,
  });

  useLoggedFeaturingQueryFailure(
    error,
    fa.mbid ?? (fa.deezer_id != null ? `dz:${fa.deezer_id}` : 'name'),
  );

  return { data, error, isLoading, isError, isRefetching, refetch };
}

type DetailPath = FeaturingDetailRoute;

type SetBusy = (busy: boolean) => void;

type ExploreArtist = {
  explore: (artist: string, detailPath: DetailPath) => Promise<void>;
  exploring: boolean;
};

function reportExploreFailure(artist: string, error: unknown): void {
  console.warn('[library] featuring explore search failed', {
    ...failureLogFields(error),
  });
  showFailureAlert(
    libraryFailureAlert(
      'library.explore_artist',
      'Search failed',
      `Could not search for ${artist}.`,
      classifyLibraryError(error),
    ),
  );
}

function topMatchQuery(artist: string) {
  return {
    q: artist,
    kinds: ['artist', 'track'] as ('artist' | 'track')[],
    limit: 1,
    saveHistory: false,
  };
}

async function searchTopMatch(
  port: LibraryPort,
  artist: string,
): Promise<DiscoveryResult | undefined> {
  const res = await port.searchDiscovery(topMatchQuery(artist));
  return res.results[0];
}

type OpenTopMatch = {
  port: LibraryPort;
  artist: string;
  detailPath: DetailPath;
  navigator: Navigator;
};

async function openTopMatch({ port, artist, detailPath, navigator }: OpenTopMatch): Promise<void> {
  try {
    const topMatch = await searchTopMatch(port, artist);
    if (topMatch !== undefined) navigator.push(detailHref(detailPath, topMatch));
  } catch (error) {
    reportExploreFailure(artist, error);
  }
}

async function whileBusy(setBusy: SetBusy, work: () => Promise<void>): Promise<void> {
  setBusy(true);
  try {
    await work();
  } finally {
    setBusy(false);
  }
}

export function useExploreArtist(): ExploreArtist {
  const navigator = useNavigator();
  const port = useLibraryPort();
  const [exploring, setExploring] = useState(false);
  const explore = async (artist: string, detailPath: DetailPath): Promise<void> => {
    if (exploring) return;
    await whileBusy(setExploring, () => openTopMatch({ port, artist, detailPath, navigator }));
  };
  return { explore, exploring };
}

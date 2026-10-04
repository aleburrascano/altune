import { useState } from 'react';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import { detailHref } from '@shared/lib/detail-handoff';
import { useNavigator, type Navigator } from '@shared/navigation';

import type { LibraryPort } from '@features/library/ports/libraryPort';
import { useLibraryPort } from '@features/library/ports/LibraryPortProvider';

import { failureLogFields } from '../domain/failureLogFields';
import type { FeaturingDetailRoute } from '../domain/featuringDetailRoute';
import { alertLibraryFailure } from '../libraryFailureAlert';
import { classifyLibraryError } from '../domain/state';

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
  alertLibraryFailure(
    'library.explore_artist',
    'Search failed',
    `Could not search for ${artist}.`,
    classifyLibraryError(error),
  );
}

const topMatchQuery = (artist: string) => ({
  q: artist,
  kinds: ['artist', 'track'] as ('artist' | 'track')[],
  limit: 1,
  saveHistory: false,
});

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

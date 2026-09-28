import { useState } from 'react';
import { useRouter } from 'expo-router';

import { searchDiscovery, type DiscoveryResult } from '@shared/api-client/discovery';
import { detailHref } from '@shared/lib/detail-handoff';

import { failureLogFields } from '../failureLogFields';
import type { FeaturingDetailRoute } from '../featuringDetailRoute';
import { alertLibraryFailure } from '../libraryFailureAlert';
import { classifyLibraryError } from '../state';

type DetailPath = FeaturingDetailRoute;

type Router = ReturnType<typeof useRouter>;

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
    'Search failed',
    `Could not search for ${artist}.`,
    classifyLibraryError(error),
  );
}

async function searchTopMatch(artist: string): Promise<DiscoveryResult | undefined> {
  const res = await searchDiscovery({
    q: artist,
    kinds: ['artist', 'track'],
    limit: 1,
    saveHistory: false,
  });
  return res.results[0];
}

async function openTopMatch(artist: string, detailPath: DetailPath, router: Router): Promise<void> {
  try {
    const topMatch = await searchTopMatch(artist);
    if (topMatch !== undefined) router.push(detailHref(detailPath, topMatch));
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
  const router = useRouter();
  const [exploring, setExploring] = useState(false);
  const explore = async (artist: string, detailPath: DetailPath): Promise<void> => {
    if (exploring) return;
    await whileBusy(setExploring, () => openTopMatch(artist, detailPath, router));
  };
  return { explore, exploring };
}

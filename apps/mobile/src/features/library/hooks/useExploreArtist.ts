import { useState } from 'react';
import { Alert } from 'react-native';
import { useRouter } from 'expo-router';

import { searchDiscovery } from '@shared/api-client/discovery';
import { detailHref } from '@shared/lib/detail-handoff';

import { failureLogFields } from '../failureLogFields';
import { classifyLibraryError, failureTail } from '../state';

type DetailPath = Parameters<typeof detailHref>[0];

function reportExploreFailure(artist: string, error: unknown): void {
  console.warn('[library] featuring explore search failed', {
    artist,
    ...failureLogFields(error),
  });
  const tail = failureTail(classifyLibraryError(error));
  Alert.alert('Search failed', `Could not search for ${artist}. ${tail}`);
}

export function useExploreArtist(): {
  explore: (artist: string, detailPath: DetailPath) => Promise<void>;
  exploring: boolean;
} {
  const router = useRouter();
  const [exploring, setExploring] = useState(false);

  const explore = async (artist: string, detailPath: DetailPath): Promise<void> => {
    if (exploring) return;
    setExploring(true);
    try {
      const res = await searchDiscovery({
        q: artist,
        kinds: ['artist', 'track'],
        limit: 1,
        saveHistory: false,
      });
      const result = res.results[0];
      if (result !== undefined) router.push(detailHref(detailPath, result));
    } catch (error) {
      reportExploreFailure(artist, error);
    } finally {
      setExploring(false);
    }
  };

  return { explore, exploring };
}

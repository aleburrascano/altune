import type { ResolvedAudioUrl } from '@shared/api-client/audio';

import type { OfflinePort } from '@shared/offline/ports/offlinePort';

export type SeededAudioUrls = Record<string, { url: string; version: string }>;

function seededEntries(urls: SeededAudioUrls, trackIds: string[]): ResolvedAudioUrl[] {
  const resolved: ResolvedAudioUrl[] = [];
  for (const trackId of trackIds) {
    const seeded = Object.hasOwn(urls, trackId) ? urls[trackId] : undefined;
    if (seeded) resolved.push({ trackId, url: seeded.url, version: seeded.version });
  }
  return resolved;
}

export function createMemoryOfflinePort({ urls }: { urls: SeededAudioUrls }): OfflinePort {
  return {
    fetchAudioUrls: (trackIds) => Promise.resolve(seededEntries(urls, trackIds)),
  };
}

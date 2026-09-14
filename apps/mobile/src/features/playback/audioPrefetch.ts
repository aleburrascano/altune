import { File } from 'expo-file-system';

import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';

import { fetchAudioUrls } from '@shared/api-client/audio';
import { parseTrackId } from '@shared/api-client/ids';
import {
  cacheDir,
  evict,
  evictCached as evictCachedFiles,
  extFromUrl,
  findCached,
} from './audioCache';
import { forgetSwap, swapUpcomingToLocal } from './nativeTrackSwap';

export {
  forgetAllSwaps,
  repairActiveToStreaming,
  swapUpcomingToLocal,
  wasSwappedToLocal,
} from './nativeTrackSwap';

const inflight = new Set<string>();
// Tracks invalidated while their prefetch was in flight: the prefetch must not swap in what it
// fetched, and deletes the track's files itself once the download has settled.
const invalidatedInflight = new Set<string>();

// The server-issued audio version is a UUID (or empty); anything else could smuggle path syntax
// into the cache file name.
const VERSION_FORMAT = /^[A-Za-z0-9_-]{0,128}$/;

export function evictCached(trackId: string): void {
  forgetSwap(trackId);
  if (inflight.has(trackId)) {
    invalidatedInflight.add(trackId);
    return;
  }
  evictCachedFiles(trackId);
}

function evictAgainstLiveQueue(): void {
  const s = useQueueStore.getState();
  evict(orderedQueueTracks(s), s.currentIndex);
}

export async function prefetchNext(activeIndex: number): Promise<void> {
  const s = useQueueStore.getState();
  const ordered = orderedQueueTracks(s);
  const next = ordered[activeIndex + 1];
  if (!next || next.source.kind !== 'library') return;
  const parsed = parseTrackId(next.source.trackId);
  if (!parsed.ok) return;
  const trackId = parsed.id;

  if (inflight.has(trackId)) return;
  inflight.add(trackId);
  try {
    const [resolved] = await fetchAudioUrls([trackId]);
    if (!resolved || !VERSION_FORMAT.test(resolved.version)) return;
    if (invalidatedInflight.has(trackId)) return;

    const existing = findCached(trackId, resolved.version);
    if (existing) {
      await swapUpcomingToLocal(next, existing.uri);
      evictAgainstLiveQueue();
      return;
    }

    const dest = new File(cacheDir(), `${trackId}.${resolved.version}${extFromUrl(resolved.url)}`);
    const file = await File.downloadFileAsync(resolved.url, dest, { idempotent: true });
    if (invalidatedInflight.has(trackId)) return;

    const s2 = useQueueStore.getState();
    const ordered2 = orderedQueueTracks(s2);
    const stillNext = ordered2[s2.currentIndex + 1];
    if (stillNext && stillNext.source.kind === 'library' && stillNext.source.trackId === trackId) {
      await swapUpcomingToLocal(stillNext, file.uri);
    }
    evict(ordered2, s2.currentIndex);
  } catch {
  } finally {
    inflight.delete(trackId);
    if (invalidatedInflight.delete(trackId)) evictCachedFiles(trackId);
  }
}

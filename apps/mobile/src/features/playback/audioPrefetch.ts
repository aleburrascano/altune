import { File } from 'expo-file-system';

import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { fetchAudioUrls } from '@shared/api-client/audio';
import { parseTrackId } from '@shared/api-client/ids';
import { REQUEST_TIMEOUT_MS } from '@shared/api-client';
import {
  MAX_PREFETCH_FILE_BYTES,
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

// In-flight prefetches by track id. The controller is the prefetch's cancellation token (the
// prefetch analogue of `loadToken`): a later prefetch whose next track differs aborts it, and
// every await boundary checks it before touching the cache or the native queue.
const inflight = new Map<string, AbortController>();
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

// A download is abandoned once no bytes have arrived for this long — the same deadline apiFetch
// gives a request — so a stalled connection cannot pin its track in `inflight` forever. Progress
// re-arms it, so a slow but live download of a large file still completes.
export const PREFETCH_STALL_TIMEOUT_MS = REQUEST_TIMEOUT_MS;

function deleteQuietly(file: File): void {
  try {
    file.delete();
  } catch {}
}

// Settles when the download does, or rejects as soon as it is aborted (superseded, stalled or
// oversized) — even if the native download ignores the abort and never settles itself.
function boundedDownload(url: string, dest: File, controller: AbortController): Promise<File> {
  const { signal } = controller;
  return new Promise<File>((resolve, reject) => {
    let stallTimer: ReturnType<typeof setTimeout> | undefined;
    const armStall = (): void => {
      clearTimeout(stallTimer);
      stallTimer = setTimeout(() => controller.abort(), PREFETCH_STALL_TIMEOUT_MS);
    };
    const onAbort = (): void => {
      clearTimeout(stallTimer);
      reject(new Error('prefetch download aborted'));
    };
    signal.addEventListener('abort', onAbort, { once: true });
    armStall();

    File.downloadFileAsync(url, dest, {
      idempotent: true,
      signal,
      onProgress: ({ bytesWritten, totalBytes }) => {
        if (Math.max(bytesWritten, totalBytes) > MAX_PREFETCH_FILE_BYTES) controller.abort();
        else armStall();
      },
    })
      .then(resolve, reject)
      .finally(() => {
        clearTimeout(stallTimer);
        signal.removeEventListener('abort', onAbort);
      });
  });
}

function upcomingLibraryTrack(
  activeIndex: number,
): { track: PlaybackTrack; trackId: string } | null {
  const track = orderedQueueTracks(useQueueStore.getState())[activeIndex + 1];
  if (!track || track.source.kind !== 'library') return null;
  const parsed = parseTrackId(track.source.trackId);
  return parsed.ok ? { track, trackId: parsed.id } : null;
}

// Cancel every in-flight prefetch whose track is no longer the one about to play.
function supersedeAllBut(trackId: string | null): void {
  for (const [id, controller] of inflight) {
    if (id !== trackId) controller.abort();
  }
}

export async function prefetchNext(activeIndex: number): Promise<void> {
  const upcoming = upcomingLibraryTrack(activeIndex);
  supersedeAllBut(upcoming?.trackId ?? null);
  if (!upcoming) return;
  const { track: next, trackId } = upcoming;

  if (inflight.has(trackId)) return;
  const controller = new AbortController();
  const { signal } = controller;
  inflight.set(trackId, controller);
  try {
    const [resolved] = await fetchAudioUrls([trackId]);
    if (!resolved || !VERSION_FORMAT.test(resolved.version)) return;
    if (signal.aborted || invalidatedInflight.has(trackId)) return;

    const existing = findCached(trackId, resolved.version);
    if (existing) {
      await swapUpcomingToLocal(next, existing.uri);
      evictAgainstLiveQueue();
      return;
    }

    const dest = new File(cacheDir(), `${trackId}.${resolved.version}${extFromUrl(resolved.url)}`);
    const file = await boundedDownload(resolved.url, dest, controller).catch(() => {
      // Timed out, superseded, oversized or failed: drop whatever part of the file was written.
      deleteQuietly(dest);
      return null;
    });
    if (!file || signal.aborted || invalidatedInflight.has(trackId)) return;

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

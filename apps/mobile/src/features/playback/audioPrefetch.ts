import { File } from 'expo-file-system';

import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { fetchAudioUrls, isAudioPrefetchEnabled } from '@shared/api-client/audio';
import { parseTrackId, type TrackId } from '@shared/api-client/ids';
import { REQUEST_TIMEOUT_MS } from '@shared/api-client';
import {
  MAX_PREFETCH_FILE_BYTES,
  buildCacheFileName,
  buildPartialCacheFileName,
  cacheDir,
  evict,
  evictAllCached,
  evictCached as evictCachedFiles,
  extFromUrl,
  findCached,
} from './audioCache';
import { forgetSwap, swapUpcomingToLocal } from './nativeTrackSwap';
import { redactedPlaybackFailure } from './redactPlaybackError';
import {
  recordPrefetchOutcome,
  type PrefetchFailureStage as PrefetchStage,
} from './playbackHealth';

// In-flight prefetches by track id. The controller is the prefetch's cancellation token (the
// prefetch analogue of `loadToken`): a later prefetch whose next track differs aborts it, and
// every await boundary checks it before touching the cache or the native queue.
const inflight = new Map<string, AbortController>();
// Tracks invalidated while their prefetch was in flight: the prefetch must not swap in what it
// fetched, and deletes the track's files itself once the download has settled.
const invalidatedInflight = new Set<string>();
// Prefetches cancelled because a different track became next: an expected outcome, not a failure.
const superseded = new WeakSet<AbortController>();

// The server-issued audio version is a UUID (or empty); anything else could smuggle path syntax
// into the cache file name.
const VERSION_FORMAT = /^[A-Za-z0-9_-]{0,128}$/;

export function evictCached(trackId: TrackId): void {
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
    let abandonedAs = 'superseded';
    const abandon = (why: string): void => {
      abandonedAs = why;
      controller.abort();
    };
    const armStall = (): void => {
      clearTimeout(stallTimer);
      stallTimer = setTimeout(() => abandon('stalled'), PREFETCH_STALL_TIMEOUT_MS);
    };
    const onAbort = (): void => {
      clearTimeout(stallTimer);
      reject(new Error(`prefetch download aborted: ${abandonedAs}`));
    };
    signal.addEventListener('abort', onAbort, { once: true });
    armStall();

    File.downloadFileAsync(url, dest, {
      idempotent: true,
      signal,
      onProgress: ({ bytesWritten, totalBytes }) => {
        if (Math.max(bytesWritten, totalBytes) > MAX_PREFETCH_FILE_BYTES) abandon('oversized');
        else armStall();
      },
    })
      .then(resolve, reject)
      .finally(() => {
        clearTimeout(stallTimer);
        signal.removeEventListener('abort', onAbort);
        // The rejection above fires on the abort, which the native download can outlive. Whatever
        // it wrote after that belongs to no prefetch, and — on sign-out — to no user still on the
        // device, so it is dropped when the download really settles rather than when we gave up.
        if (signal.aborted) deleteQuietly(dest);
      });
  });
}

function upcomingLibraryTrack(
  activeIndex: number,
): { track: PlaybackTrack; trackId: TrackId } | null {
  const track = orderedQueueTracks(useQueueStore.getState())[activeIndex + 1];
  if (!track || track.source.kind !== 'library') return null;
  const parsed = parseTrackId(track.source.trackId);
  return parsed.ok ? { track, trackId: parsed.id } : null;
}

// Cancel every in-flight prefetch whose track is no longer the one about to play.
function supersedeAllBut(trackId: TrackId | null): void {
  for (const [id, controller] of inflight) {
    if (id === trackId) continue;
    superseded.add(controller);
    controller.abort();
  }
}

/**
 * Drops the prefetched audio of the user who is leaving, on sign-out or an account switch —
 * `evict` only ever runs off a later prefetch, so without this the files sit unencrypted on a
 * shared device until one happens (#1722). Cancel first, wipe second: a download cancelled
 * after the wipe lands its file in a directory that has already been emptied.
 */
export function discardPrefetchedAudio(): void {
  supersedeAllBut(null);
  evictAllCached();
}

// A failed prefetch leaves the track streaming, which still plays; this trace is the only record
// that the fallback fired. One stable message so failures can be counted by stage, and each is
// tallied into the playback health metric. A native download failure names the URL it could not
// fetch, so the rejection is redacted before it reaches the log — see redactedPlaybackFailure.
function tracePrefetchFailure(stage: PrefetchStage, trackId: TrackId, error: unknown): void {
  console.warn('[playback] prefetch failed', {
    stage,
    trackId,
    error: redactedPlaybackFailure(error),
  });
  recordPrefetchOutcome(stage);
}

type ResolvedAudio = { url: string; version: string };

class StageFailure {
  constructor(
    readonly stage: PrefetchStage,
    readonly cause: unknown,
  ) {}
}

function inStage<T>(stage: PrefetchStage, work: Promise<T>): Promise<T> {
  return work.catch((err: unknown) => {
    throw new StageFailure(stage, err);
  });
}

function atStage<T>(stage: PrefetchStage, work: () => T): T {
  try {
    return work();
  } catch (err) {
    throw new StageFailure(stage, err);
  }
}

function isCancelled(trackId: TrackId, controller: AbortController): boolean {
  return controller.signal.aborted || invalidatedInflight.has(trackId);
}

async function resolveAudio(
  trackId: TrackId,
  controller: AbortController,
): Promise<ResolvedAudio | null> {
  const [resolved] = await inStage('resolve', fetchAudioUrls([trackId]));
  if (!resolved || !VERSION_FORMAT.test(resolved.version)) return null;
  if (!isAudioPrefetchEnabled() || isCancelled(trackId, controller)) return null;
  return resolved;
}

async function swapCachedHit(next: PlaybackTrack, uri: string): Promise<void> {
  try {
    await swapUpcomingToLocal(next, uri);
    evictAgainstLiveQueue();
  } catch (err) {
    throw new StageFailure('swap', err);
  }
  recordPrefetchOutcome('ok');
}

function downloadFailed(
  { trackId, controller }: ClaimedPrefetch,
  partial: File,
  err: unknown,
): null {
  if (!superseded.has(controller)) tracePrefetchFailure('download', trackId, err);
  deleteQuietly(partial);
  return null;
}

function cacheFileFor(trackId: TrackId, resolved: ResolvedAudio, partial: boolean): File {
  const build = partial ? buildPartialCacheFileName : buildCacheFileName;
  return new File(cacheDir(), build(trackId, resolved.version, extFromUrl(resolved.url)));
}

function moveIntoCache(trackId: TrackId, resolved: ResolvedAudio, partial: File): File {
  try {
    const file = cacheFileFor(trackId, resolved, false);
    partial.moveSync(file, { overwrite: true });
    return file;
  } catch (err) {
    deleteQuietly(partial);
    throw new StageFailure('download', err);
  }
}

async function downloadAndSwap(claimed: ClaimedPrefetch, resolved: ResolvedAudio): Promise<void> {
  const { trackId, controller } = claimed;
  const partial = atStage('download', () => cacheFileFor(trackId, resolved, true));
  const downloaded = await atStage('download', () =>
    boundedDownload(resolved.url, partial, controller),
  ).catch((err: unknown) => downloadFailed(claimed, partial, err));
  if (!downloaded) return;
  if (isCancelled(trackId, controller)) return deleteQuietly(partial);
  await swapDownloaded(trackId, moveIntoCache(trackId, resolved, partial));
}

function queueSnapshot(trackId: TrackId) {
  const s = useQueueStore.getState();
  const ordered = orderedQueueTracks(s);
  const next = ordered[s.currentIndex + 1];
  const stillNext = next?.source.kind === 'library' && next.source.trackId === trackId;
  return { ordered, index: s.currentIndex, stillNext: stillNext ? next : undefined };
}

async function swapDownloaded(trackId: TrackId, file: File): Promise<void> {
  try {
    const q = queueSnapshot(trackId);
    if (q.stillNext) await swapUpcomingToLocal(q.stillNext, file.uri);
    evict(q.ordered, q.index);
  } catch (err) {
    throw new StageFailure('swap', err);
  }
  recordPrefetchOutcome('ok');
}

interface ClaimedPrefetch {
  track: PlaybackTrack;
  trackId: TrackId;
  controller: AbortController;
}

async function runPrefetch(claimed: ClaimedPrefetch): Promise<void> {
  const { track, trackId, controller } = claimed;
  const resolved = await resolveAudio(trackId, controller);
  if (!resolved) return;
  const existing = atStage('swap', () => findCached(trackId, resolved.version));
  if (existing) await swapCachedHit(track, existing.uri);
  else await downloadAndSwap(claimed, resolved);
}

function wantedUpcoming(activeIndex: number) {
  const upcoming = isAudioPrefetchEnabled() ? upcomingLibraryTrack(activeIndex) : null;
  supersedeAllBut(upcoming?.trackId ?? null);
  return upcoming && !inflight.has(upcoming.trackId) ? upcoming : null;
}

function claimUpcoming(activeIndex: number): ClaimedPrefetch | null {
  const upcoming = wantedUpcoming(activeIndex);
  if (!upcoming) return null;
  const controller = new AbortController();
  inflight.set(upcoming.trackId, controller);
  return { ...upcoming, controller };
}

function traceStageFailure(trackId: TrackId, err: unknown): void {
  const failure = err instanceof StageFailure ? err : new StageFailure('resolve', err);
  tracePrefetchFailure(failure.stage, trackId, failure.cause);
}

function releaseInflight(trackId: TrackId): void {
  inflight.delete(trackId);
  if (invalidatedInflight.delete(trackId)) evictCachedFiles(trackId);
}

async function settlePrefetch(claimed: ClaimedPrefetch): Promise<void> {
  try {
    await runPrefetch(claimed);
  } catch (err) {
    traceStageFailure(claimed.trackId, err);
  } finally {
    releaseInflight(claimed.trackId);
  }
}

export function prefetchNext(activeIndex: number): Promise<void> {
  const claimed = claimUpcoming(activeIndex);
  return claimed ? settlePrefetch(claimed) : Promise.resolve();
}

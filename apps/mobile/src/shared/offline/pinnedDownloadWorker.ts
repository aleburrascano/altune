import { fetchAudioUrls } from '@shared/api-client/audio';
import { equalJitterMs, isRetryable } from '@shared/errors';
import type { TrackId } from '@shared/api-client/ids';
import { isLoopEnabled } from '@shared/killSwitch/killSwitch';

import {
  deletePinned,
  downloadPinned,
  pinStorageFull,
  unsignedUrl,
  withPinnedBytesCached,
} from './pinnedFiles';
import {
  type PinnedEntry,
  downloadingEntry,
  failedEntry,
  flushIndex,
  readyEntry,
  scheduleSaveIndex,
} from './pinnedIndex';

// The sequential background download worker: drains the store's queue one track
// at a time, guarded by isWorking so concurrent triggers never run two drains.
// The offline-download kill switch is read before each track: switched off, the
// drain stops and leaves the rest queued (a download already running finishes),
// and the store restarts the drain when the switch comes back on. A track's
// transient failure is retried in place, so the tracks behind it wait out its
// backoff rather than being started concurrently.

type QueueState = {
  entries: Record<string, PinnedEntry>;
  queue: TrackId[];
  isWorking: boolean;
};

type Setter = (partial: Partial<QueueState> | ((s: QueueState) => Partial<QueueState>)) => void;
type Getter = () => QueueState;

export async function runDownloadQueue(set: Setter, get: Getter): Promise<void> {
  if (get().isWorking) return;
  set({ isWorking: true });
  try {
    // One drain measures the pinned directory once: the isWorking guard above makes it the only
    // pass open, so its running byte total is what every track's room check reads.
    await withPinnedBytesCached(() => drainQueue(set, get));
  } finally {
    set({ isWorking: false });
    // The drain's coalesced status transitions reach disk as soon as the queue is empty.
    flushIndex();
  }
}

async function drainQueue(set: Setter, get: Getter): Promise<void> {
  for (;;) {
    const trackId = get().queue[0];
    if (trackId === undefined || !isLoopEnabled('offlineDownloads')) break;
    set((s) => ({ queue: s.queue.slice(1) }));
    await downloadOne(trackId, set, get);
  }
}

async function downloadOne(trackId: TrackId, set: Setter, get: Getter): Promise<void> {
  if (get().entries[trackId] === undefined) return;

  const mark = (entry: PinnedEntry): void => {
    set((s) => {
      if (s.entries[trackId] === undefined) return {};
      const entries = { ...s.entries, [trackId]: entry };
      scheduleSaveIndex(entries);
      return { entries };
    });
  };

  mark(downloadingEntry(trackId));
  const isStillDownloading = (): boolean => get().entries[trackId]?.status === 'downloading';

  const outcome = await downloadWithRetries(trackId, isStillDownloading);

  const supersededWhileDownloading = !isStillDownloading();
  if (supersededWhileDownloading) {
    deletePinned(trackId);
    return;
  }

  mark(outcome.ok ? readyEntry(trackId, outcome.uri, outcome.version) : failedEntry(trackId));
}

/** The tries one track gets: its first attempt, plus the retries a transient failure earns. */
const MAX_DOWNLOAD_ATTEMPTS = 3;
/** The first retry waits between half of this and this; each later one doubles that window. */
export const DOWNLOAD_RETRY_BASE_MS = 2_000;

/**
 * Delay before retry number `retry` (1-based): exponential from DOWNLOAD_RETRY_BASE_MS with
 * equal jitter, so the wait lands in [ceiling/2, ceiling] and a batch's failures spread out
 * instead of re-hitting a recovering network together. `random` is a sample in [0, 1).
 */
function downloadBackoffMs(retry: number, random: number): number {
  return equalJitterMs(DOWNLOAD_RETRY_BASE_MS, Infinity, retry - 1, random);
}

// Null when the failure is permanent (no room, no signed url, a rejected id) or the track has
// spent its attempts: both mean the entry is marked failed now rather than waiting again.
function retryDelayMs(error: unknown, attemptNumber: number): number | null {
  if (attemptNumber >= MAX_DOWNLOAD_ATTEMPTS || !isRetryable(error)) return null;
  return downloadBackoffMs(attemptNumber, Math.random());
}

// Waits out the backoff and reports whether the track is still worth a further attempt: an unpin
// (or a sign-out) landing during the wait takes the entry off 'downloading' and ends the retries.
function stillDownloadingAfter(ms: number, isStillDownloading: () => boolean): Promise<boolean> {
  return new Promise((resolve) => setTimeout(() => resolve(isStillDownloading()), ms));
}

type FailedAttempt = { ok: false; error: unknown; url: string | undefined };
type Attempt = { ok: true; uri: string; version: string } | FailedAttempt;

function warnAttemptFailed(trackId: TrackId, attempt: FailedAttempt, retryIn: number | null): void {
  const retry = retryIn === null ? '' : `, retrying in ${retryIn}ms`;
  console.warn(`[offline] pinned download failed for track ${trackId}${retry}`, {
    url: unsignedUrl(attempt.url),
    error: attempt.error,
  });
}

/**
 * Reattempts a failure `isRetryable` classifies as transient — a dropped connection, a 5xx, a
 * 429 — so a wifi blip during a batch does not permanently fail the track. A permanent failure
 * returns on its first attempt. Retrying stops once the entry is no longer downloading, so an
 * unpin landing during a backoff wait is not followed by another transfer.
 */
async function downloadWithRetries(
  trackId: TrackId,
  isStillDownloading: () => boolean,
  attemptNumber = 1,
): Promise<Attempt> {
  const outcome = await attemptDownload(trackId);
  if (outcome.ok) return outcome;
  const retryIn = retryDelayMs(outcome.error, attemptNumber);
  warnAttemptFailed(trackId, outcome, retryIn);
  if (retryIn === null) return outcome;
  if (!(await stillDownloadingAfter(retryIn, isStillDownloading))) return outcome;
  return downloadWithRetries(trackId, isStillDownloading, attemptNumber + 1);
}

async function attemptDownload(trackId: TrackId): Promise<Attempt> {
  let url: string | undefined;
  try {
    // Storage can fill while a batch drains, so the room check is repeated at each track's turn.
    if (pinStorageFull()) throw new Error('pinned storage is full');
    const [resolved] = await fetchAudioUrls([trackId]);
    if (!resolved) throw new Error('no signed url');
    url = resolved.url;
    const uri = await downloadPinned(trackId, resolved.url);
    return { ok: true, uri, version: resolved.version };
  } catch (error) {
    return { ok: false, error, url };
  }
}

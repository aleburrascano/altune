import { fetchAudioUrls } from '@shared/api-client/audio';
import { equalJitterMs } from '@shared/backoff';
import { isRetryable } from '@shared/errors';
import type { TrackId } from '@shared/api-client/ids';
import { isLoopEnabled } from '@shared/killSwitch/killSwitch';
import { recordEvent } from '@shared/telemetry/recordEvent';

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
  tagChangedEntries,
} from './pinnedIndex';

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
    await withPinnedBytesCached(() => drainQueue(set, get));
  } finally {
    set({ isWorking: false });
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
      tagChangedEntries(entries, [trackId]);
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

const MAX_DOWNLOAD_ATTEMPTS = 3;
export const DOWNLOAD_RETRY_BASE_MS = 2_000;

function downloadBackoffMs(retry: number, random: number): number {
  return equalJitterMs(DOWNLOAD_RETRY_BASE_MS, Infinity, retry - 1, random);
}

function retryDelayMs(error: unknown, attemptNumber: number): number | null {
  if (attemptNumber >= MAX_DOWNLOAD_ATTEMPTS || !isRetryable(error)) return null;
  return downloadBackoffMs(attemptNumber, Math.random());
}

function stillDownloadingAfter(ms: number, isStillDownloading: () => boolean): Promise<boolean> {
  return new Promise((resolve) => setTimeout(() => resolve(isStillDownloading()), ms));
}

type FailedAttempt = { ok: false; error: unknown; url: string | undefined };
type Attempt = { ok: true; uri: string; version: string } | FailedAttempt;

function warnAttemptFailed(trackId: TrackId, attempt: FailedAttempt, retryIn: number | null): void {
  const retry = retryIn === null ? '' : `, retrying in ${retryIn}ms`;
  recordEvent({
    type: 'download_failed',
    payload: { track_id: trackId, will_retry: retryIn !== null },
  }).catch(() => undefined);
  console.warn(`[offline] pinned download failed for track ${trackId}${retry}`, {
    url: unsignedUrl(attempt.url),
    error: attempt.error,
  });
}

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

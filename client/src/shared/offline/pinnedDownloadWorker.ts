import type { ResolvedAudioUrl } from '@shared/api-client/audio';
import { equalJitterMs } from '@shared/backoff';
import { isRetryable } from '@shared/failure/errors';
import type { TrackId } from '@shared/api-client/ids';
import { isLoopEnabled } from '@shared/killSwitch/killSwitch';
import { recordEvent } from '@shared/telemetry/recordEvent';

import {
  deletePinned,
  downloadPinned,
  pinStorageFull,
  unsignedUrl,
  withPinnedBytesCached,
} from '@shared/offline/pinnedFiles';
import type { OfflinePort } from '@shared/offline/ports/offlinePort';
import {
  type PinnedEntry,
  downloadingEntry,
  failedEntry,
  flushIndex,
  readyEntry,
  scheduleSaveIndex,
  tagChangedEntries,
} from '@shared/offline/pinnedIndex';

type QueueState = {
  entries: Record<string, PinnedEntry>;
  queue: TrackId[];
  isWorking: boolean;
};

type Setter = (partial: Partial<QueueState> | ((s: QueueState) => Partial<QueueState>)) => void;
type Getter = () => QueueState;

export async function runDownloadQueue(set: Setter, get: Getter, port: OfflinePort): Promise<void> {
  if (get().isWorking) return;
  set({ isWorking: true });
  try {
    await withPinnedBytesCached(() => drainQueue(set, get, port));
  } finally {
    set({ isWorking: false });
    flushIndex();
  }
}

async function drainQueue(set: Setter, get: Getter, port: OfflinePort): Promise<void> {
  for (;;) {
    const trackId = get().queue[0];
    if (trackId === undefined || !isLoopEnabled('offlineDownloads')) break;
    set((s) => ({ queue: s.queue.slice(1) }));
    await downloadOne(trackId, set, get, port);
  }
}

function markEntry(set: Setter, trackId: TrackId, entry: PinnedEntry): void {
  set((s) => {
    if (s.entries[trackId] === undefined) return {};
    const entries = { ...s.entries, [trackId]: entry };
    tagChangedEntries(entries, [trackId]);
    scheduleSaveIndex(entries);
    return { entries };
  });
}

function jobFor(trackId: TrackId, get: Getter, port: OfflinePort): DownloadJob {
  const isStillDownloading = (): boolean => get().entries[trackId]?.status === 'downloading';
  return { trackId, isStillDownloading, port };
}

function settledEntry(trackId: TrackId, outcome: Attempt): PinnedEntry {
  return outcome.ok ? readyEntry(trackId, outcome.uri, outcome.version) : failedEntry(trackId);
}

async function settleDownload(set: Setter, job: DownloadJob): Promise<void> {
  const outcome = await downloadWithRetries(job);
  if (!job.isStillDownloading()) {
    deletePinned(job.trackId);
    return;
  }
  markEntry(set, job.trackId, settledEntry(job.trackId, outcome));
}

async function downloadOne(
  trackId: TrackId,
  set: Setter,
  get: Getter,
  port: OfflinePort,
): Promise<void> {
  if (get().entries[trackId] === undefined) return;
  markEntry(set, trackId, downloadingEntry(trackId));
  await settleDownload(set, jobFor(trackId, get, port));
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

function recordDownloadFailed(trackId: TrackId, willRetry: boolean): void {
  recordEvent({
    type: 'download_failed',
    payload: { track_id: trackId, will_retry: willRetry },
  }).catch(() => undefined);
}

function warnAttemptFailed(trackId: TrackId, attempt: FailedAttempt, retryIn: number | null): void {
  const retry = retryIn === null ? '' : `, retrying in ${retryIn}ms`;
  recordDownloadFailed(trackId, retryIn !== null);
  console.warn(`[offline] pinned download failed for track ${trackId}${retry}`, {
    url: unsignedUrl(attempt.url),
    error: attempt.error,
  });
}

type DownloadJob = {
  trackId: TrackId;
  isStillDownloading: () => boolean;
  port: OfflinePort;
};

async function downloadWithRetries(job: DownloadJob, attemptNumber = 1): Promise<Attempt> {
  const outcome = await attemptDownload(job);
  if (outcome.ok) return outcome;
  const retryIn = retryDelayMs(outcome.error, attemptNumber);
  warnAttemptFailed(job.trackId, outcome, retryIn);
  if (retryIn === null) return outcome;
  if (!(await stillDownloadingAfter(retryIn, job.isStillDownloading))) return outcome;
  return downloadWithRetries(job, attemptNumber + 1);
}

async function resolveSignedUrl(job: DownloadJob): Promise<ResolvedAudioUrl> {
  if (pinStorageFull()) throw new Error('pinned storage is full');
  const [resolved] = await job.port.fetchAudioUrls([job.trackId]);
  if (!resolved) throw new Error('no signed url');
  return resolved;
}

async function downloadResolved(trackId: TrackId, resolved: ResolvedAudioUrl): Promise<Attempt> {
  const uri = await downloadPinned(trackId, resolved.url);
  return { ok: true, uri, version: resolved.version };
}

async function attemptDownload(job: DownloadJob): Promise<Attempt> {
  let url: string | undefined;
  try {
    const resolved = await resolveSignedUrl(job);
    url = resolved.url;
    return await downloadResolved(job.trackId, resolved);
  } catch (error) {
    return { ok: false, error, url };
  }
}

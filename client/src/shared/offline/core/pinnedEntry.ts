import { isSafeId, parseTrackId, type TrackId } from '@shared/catalog';

export type PinnedEntry =
  | { trackId: TrackId; status: 'ready'; uri: string; version?: string }
  | { trackId: TrackId; status: 'failed' }
  | { trackId: TrackId; status: 'queued' | 'downloading' };

export type PinnedStatus = PinnedEntry['status'];

export type NarrowedIndex = { entries: Record<string, PinnedEntry>; dropped: number };

const changedIdsByEntries = new WeakMap<Record<string, PinnedEntry>, readonly string[]>();

export function tagChangedEntries(
  entries: Record<string, PinnedEntry>,
  ids: readonly string[],
): void {
  changedIdsByEntries.set(entries, ids);
}

export function changedEntryIds(
  entries: Record<string, PinnedEntry>,
): readonly string[] | undefined {
  return changedIdsByEntries.get(entries);
}

export function readyEntry(trackId: TrackId, uri: string, version?: string): PinnedEntry {
  if (version === undefined) return { trackId, status: 'ready', uri };
  return { trackId, status: 'ready', uri, version };
}

export function queuedEntry(trackId: TrackId): PinnedEntry {
  return { trackId, status: 'queued' };
}

export function downloadingEntry(trackId: TrackId): PinnedEntry {
  return { trackId, status: 'downloading' };
}

export function failedEntry(trackId: TrackId): PinnedEntry {
  return { trackId, status: 'failed' };
}

export function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

const PINNED_STATUSES: Record<PinnedStatus, true> = {
  queued: true,
  downloading: true,
  ready: true,
  failed: true,
};

function isPinnedStatus(value: unknown): value is PinnedStatus {
  return typeof value === 'string' && PINNED_STATUSES[value as PinnedStatus] === true;
}

function entryOfStatus(
  trackId: TrackId,
  status: PinnedStatus,
  uri?: string,
  version?: string,
): PinnedEntry | null {
  switch (status) {
    case 'ready':
      return uri === undefined ? null : readyEntry(trackId, uri, version);
    case 'failed':
      return failedEntry(trackId);
    case 'queued':
      return queuedEntry(trackId);
    case 'downloading':
      return downloadingEntry(trackId);
  }
}

function narrowEntry(value: unknown): PinnedEntry | null {
  if (typeof value !== 'object' || value === null) return null;
  const { trackId: rawTrackId, status, uri, version } = value as Record<string, unknown>;
  if (typeof rawTrackId !== 'string' || !isPinnedStatus(status)) return null;
  if (uri !== undefined && typeof uri !== 'string') return null;
  if (version !== undefined && typeof version !== 'string') return null;
  const trackId = parseTrackId(rawTrackId);
  if (!trackId.ok) return null;
  return entryOfStatus(trackId.value, status, uri, version);
}

export function narrowIndex(parsed: Record<string, unknown>): NarrowedIndex {
  const entries: Record<string, PinnedEntry> = {};
  let dropped = 0;
  for (const [trackId, value] of Object.entries(parsed)) {
    const entry = isSafeId(trackId) ? narrowEntry(value) : null;
    if (entry === null) dropped += 1;
    else entries[trackId] = entry;
  }
  return { entries, dropped };
}

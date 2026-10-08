import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/playbackTrack';
import type { QueueSource, RepeatMode } from '@shared/playback/queueSource';

type TrackList = readonly PlaybackTrack[];

export interface QueueState {
  tracks: readonly PlaybackTrack[];
  playOrder: readonly number[];
  upNext: readonly number[];
  appended: readonly number[];
  currentIndex: number;
  repeatMode: RepeatMode;
  shuffled: boolean;
  source: QueueSource | null;
  resumePositionMs: number;
  generation: number;
}

export function identityOrder(length: number): number[] {
  return Array.from({ length }, (_, i) => i);
}

export interface UserQueued {
  upNext: readonly number[];
  appended: readonly number[];
}

function partitionTail(tail: readonly number[], queued: UserQueued) {
  const nextIds = new Set(queued.upNext);
  const lastIds = new Set(queued.appended);
  const rest = tail.filter((i) => !nextIds.has(i) && !lastIds.has(i));
  const next = tail.filter((i) => nextIds.has(i));
  const last = tail.filter((i) => lastIds.has(i));
  return { next, rest, last };
}

export function reorderTail(
  order: readonly number[],
  keepThrough: number,
  queued: UserQueued,
  arrange: (rest: number[]) => void,
): number[] {
  const { next, rest, last } = partitionTail(order.slice(keepThrough + 1), queued);
  arrange(rest);
  return [...order.slice(0, keepThrough + 1), ...next, ...rest, ...last];
}

export function fisherYates(items: number[]): void {
  for (let i = items.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    const tmp = items[i]!;
    items[i] = items[j]!;
    items[j] = tmp;
  }
}

export function ascending(items: number[]): void {
  items.sort((a, b) => a - b);
}

export function withAppended(state: QueueState, tracks: TrackList): Partial<QueueState> {
  const added = tracks.map((_, i) => state.tracks.length + i);
  return {
    tracks: [...state.tracks, ...tracks],
    playOrder: [...state.playOrder, ...added],
    appended: [...state.appended, ...added],
  };
}

export function withoutTrack(indices: readonly number[], removed: number): number[] {
  return indices.filter((i) => i !== removed).map((i) => (i > removed ? i - 1 : i));
}

export function currentAfterMove(currentIndex: number, fromIndex: number, toIndex: number): number {
  if (fromIndex === currentIndex) return toIndex;
  if (fromIndex < currentIndex && toIndex >= currentIndex) return currentIndex - 1;
  if (fromIndex > currentIndex && toIndex <= currentIndex) return currentIndex + 1;
  return currentIndex;
}

export function movedOrder(
  playOrder: readonly number[],
  fromIndex: number,
  toIndex: number,
): number[] {
  const order = [...playOrder];
  const [moved] = order.splice(fromIndex, 1);
  order.splice(toIndex, 0, moved!);
  return order;
}

export function currentAfterRemoval(
  currentIndex: number,
  removedPosition: number,
  newLength: number,
): number {
  if (removedPosition < currentIndex) return currentIndex - 1;
  if (removedPosition === currentIndex) return Math.min(currentIndex, newLength - 1);
  return currentIndex;
}

interface FreshQueueOptions {
  tracks: readonly PlaybackTrack[];
  playOrder: readonly number[];
  currentIndex: number;
  shuffled: boolean;
  source: QueueSource | null;
  generation: number;
}

const NOTHING_QUEUED = { upNext: [], appended: [], resumePositionMs: 0 };

export function freshQueue({
  generation,
  ...loaded
}: FreshQueueOptions): Omit<QueueState, 'repeatMode'> {
  return { ...loaded, ...NOTHING_QUEUED, generation: generation + 1 };
}

export function survivingPositions(
  indices: readonly number[],
  renumbered: ReadonlyMap<number, number>,
): number[] {
  return indices.map((i) => renumbered.get(i)).filter((at) => at != null);
}

export function withoutUpcoming(state: QueueState, kept: readonly number[]): Partial<QueueState> {
  const renumbered = new Map(kept.map((trackIdx, position) => [trackIdx, position]));
  return {
    tracks: kept.map((i) => state.tracks[i]!),
    playOrder: identityOrder(kept.length),
    upNext: survivingPositions(state.upNext, renumbered),
    appended: survivingPositions(state.appended, renumbered),
    shuffled: state.shuffled && kept.length > 1,
  };
}

export function isPositionIn(length: number, index: number): boolean {
  return Number.isInteger(index) && index >= 0 && index < length;
}

export function trackAt(
  tracks: readonly PlaybackTrack[],
  playOrder: readonly number[],
  index: number,
): PlaybackTrack | null {
  const trackIndex = playOrder[index];
  if (trackIndex == null) return null;
  return tracks[trackIndex] ?? null;
}

export function nearestKeyPosition(
  ordered: readonly (PlaybackTrack | null)[],
  key: string,
  preferred: number,
): number | null {
  const hits = ordered.map((t, i) => (t && trackKey(t) === key ? i : -1)).filter((i) => i >= 0);
  if (hits.length === 0) return null;
  return hits.reduce((a, b) => (Math.abs(b - preferred) < Math.abs(a - preferred) ? b : a));
}

function orderedTracks(tracks: TrackList, playOrder: readonly number[]) {
  return playOrder.map((i) => tracks[i] ?? null);
}

function keyedTarget(
  ordered: readonly (PlaybackTrack | null)[],
  key: string,
  index: number,
): number | null {
  const at = ordered[index];
  if (at && trackKey(at) === key) return index;
  return nearestKeyPosition(ordered, key, index);
}

export function resolveSyncTarget(
  tracks: TrackList,
  playOrder: readonly number[],
  index: number,
  key: string | undefined,
): number | null {
  const inRange = index >= 0 && index < playOrder.length;
  if (key == null) return inRange ? index : null;
  return keyedTarget(orderedTracks(tracks, playOrder), key, index);
}

export const REPEAT_CYCLE: Record<RepeatMode, RepeatMode> = {
  off: 'all',
  all: 'one',
  one: 'off',
};

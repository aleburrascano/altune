import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack, QueueSource, RepeatMode } from '@shared/playback/types';

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

export function reorderTail(
  order: readonly number[],
  keepThrough: number,
  queued: UserQueued,
  arrange: (rest: number[]) => void,
): number[] {
  const tail = order.slice(keepThrough + 1);
  const nextIds = new Set(queued.upNext);
  const lastIds = new Set(queued.appended);
  const rest = tail.filter((i) => !nextIds.has(i) && !lastIds.has(i));
  arrange(rest);
  const next = tail.filter((i) => nextIds.has(i));
  const last = tail.filter((i) => lastIds.has(i));
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

export function withAppended(
  state: QueueState,
  tracks: readonly PlaybackTrack[],
): Partial<QueueState> {
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

export function survivingPositions(
  indices: readonly number[],
  renumbered: ReadonlyMap<number, number>,
): number[] {
  return indices.flatMap((i) => {
    const at = renumbered.get(i);
    return at == null ? [] : [at];
  });
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
  const hits = ordered.flatMap((t, i) => (t && trackKey(t) === key ? [i] : []));
  if (hits.length === 0) return null;
  return hits.reduce((a, b) => (Math.abs(b - preferred) < Math.abs(a - preferred) ? b : a));
}

export function resolveSyncTarget(
  tracks: readonly PlaybackTrack[],
  playOrder: readonly number[],
  index: number,
  key: string | undefined,
): number | null {
  const inRange = index >= 0 && index < playOrder.length;
  if (key == null) return inRange ? index : null;
  const ordered = playOrder.map((i) => tracks[i] ?? null);
  const at = inRange ? ordered[index] : null;
  if (at && trackKey(at) === key) return index;
  return nearestKeyPosition(ordered, key, index);
}

export const REPEAT_CYCLE: Record<RepeatMode, RepeatMode> = {
  off: 'all',
  all: 'one',
  one: 'off',
};

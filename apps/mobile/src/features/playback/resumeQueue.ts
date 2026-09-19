import { clamp } from './clamp';

// A queue can hold the same track more than once, so an id alone does not say which copy
// was playing. The saved cursor is resolved to (id, occurrence): the id at the saved index
// and how many earlier copies of that id precede it. Dropping unavailable tracks removes
// every copy of an id together, so the occurrence rank survives the rebuild filters.

function savedCursor(savedTrackIds: readonly string[], savedCurrentIndex: number): number {
  return savedTrackIds[savedCurrentIndex] === undefined ? 0 : savedCurrentIndex;
}

export function currentTrackId(
  savedTrackIds: readonly string[],
  savedCurrentIndex: number,
): string {
  return savedTrackIds[savedCursor(savedTrackIds, savedCurrentIndex)] ?? '';
}

export function currentOccurrence(
  savedTrackIds: readonly string[],
  savedCurrentIndex: number,
): number {
  const cursor = savedCursor(savedTrackIds, savedCurrentIndex);
  const id = savedTrackIds[cursor];
  return savedTrackIds.slice(0, cursor).filter((other) => other === id).length;
}

// Index of the `occurrence`-th copy of `id` (0-based), the last copy when there are
// fewer, or -1 when `id` is absent.
function occurrenceIndex(ids: readonly string[], id: string, occurrence: number): number {
  let found = -1;
  let seen = 0;
  for (let i = 0; i < ids.length && seen <= occurrence; i++) {
    if (ids[i] !== id) continue;
    found = i;
    seen++;
  }
  return found;
}

export function resolveResumeStartIndex(
  savedTrackIds: readonly string[],
  savedCurrentIndex: number,
  validTrackIds: readonly string[],
): number {
  if (validTrackIds.length === 0) return 0;
  const currentId = currentTrackId(savedTrackIds, savedCurrentIndex);
  const occurrence = currentOccurrence(savedTrackIds, savedCurrentIndex);
  const found = currentId ? occurrenceIndex(validTrackIds, currentId, occurrence) : -1;
  if (found >= 0) return found;
  return clamp(savedCurrentIndex, 0, validTrackIds.length - 1);
}

// Hands out natural-order positions per id: the n-th play copy of an id takes the n-th
// natural copy, so duplicates map to distinct tracks (reusing the last copy if the play
// order holds more copies than the natural order).
function naturalPositions(naturalIds: readonly string[]): (id: string) => number | undefined {
  const positions = new Map<string, number[]>();
  naturalIds.forEach((id, i) => {
    const list = positions.get(id);
    if (list) list.push(i);
    else positions.set(id, [i]);
  });
  const taken = new Map<string, number>();
  return (id) => {
    const list = positions.get(id);
    if (!list) return undefined;
    const n = taken.get(id) ?? 0;
    taken.set(id, n + 1);
    return list[Math.min(n, list.length - 1)];
  };
}

export function reconstructPlayOrder(
  naturalIds: readonly string[],
  playIds: readonly string[],
  currentId: string,
  currentOccurrenceRank: number,
): { playOrder: number[]; currentIndex: number } {
  const nextNatural = naturalPositions(naturalIds);
  const playOrder: number[] = [];
  let currentIndex = 0;
  let currentSeen = 0;
  for (const id of playIds) {
    const ni = nextNatural(id);
    if (ni === undefined) continue;
    if (id === currentId && currentSeen++ <= currentOccurrenceRank) {
      currentIndex = playOrder.length;
    }
    playOrder.push(ni);
  }
  return { playOrder, currentIndex };
}

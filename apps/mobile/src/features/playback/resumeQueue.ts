import { clamp } from './clamp';

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

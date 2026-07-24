export function currentTrackId(
  savedTrackIds: readonly string[],
  savedCurrentIndex: number,
): string {
  return savedTrackIds[savedCurrentIndex] ?? savedTrackIds[0] ?? '';
}

export function resolveResumeStartIndex(
  savedTrackIds: readonly string[],
  savedCurrentIndex: number,
  validTrackIds: readonly string[],
): number {
  if (validTrackIds.length === 0) return 0;
  const currentId = currentTrackId(savedTrackIds, savedCurrentIndex);
  const found = currentId ? validTrackIds.indexOf(currentId) : -1;
  if (found >= 0) return found;
  return Math.max(0, Math.min(savedCurrentIndex, validTrackIds.length - 1));
}

export function reconstructPlayOrder(
  naturalIds: readonly string[],
  playIds: readonly string[],
  currentId: string,
): { playOrder: number[]; currentIndex: number } {
  const naturalIndex = new Map<string, number>();
  naturalIds.forEach((id, i) => naturalIndex.set(id, i));

  const playOrder: number[] = [];
  let currentIndex = 0;
  for (const id of playIds) {
    const ni = naturalIndex.get(id);
    if (ni === undefined) continue;
    if (id === currentId) currentIndex = playOrder.length;
    playOrder.push(ni);
  }
  return { playOrder, currentIndex };
}

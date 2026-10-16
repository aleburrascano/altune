import { clamp } from '@shared/math/math';

export type BatchOutcome<T> = { succeeded: readonly T[]; failed: readonly T[] };

function laneCount(limit: number, itemCount: number): number {
  return clamp(limit, 1, itemCount);
}

export async function runBounded<T>(
  items: readonly T[],
  limit: number,
  worker: (item: T) => Promise<unknown>,
  shouldContinue: () => boolean = () => true,
): Promise<BatchOutcome<T>> {
  const queue = [...items];
  const succeeded: T[] = [];
  const failed: T[] = [];
  const drain = async (): Promise<void> => {
    if (!shouldContinue()) return;
    const item = queue.shift();
    if (item === undefined) return;
    await worker(item).then(
      () => void succeeded.push(item),
      () => void failed.push(item),
    );
    await drain();
  };
  await Promise.all(Array.from({ length: laneCount(limit, queue.length) }, () => drain()));
  return { succeeded, failed };
}

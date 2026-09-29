interface PendingLoad {
  readonly generation: number;
  readonly targetIndex: number;
}

let generationSeq = 0;
let pending: readonly PendingLoad[] = [];

export function beginNativeLoad(targetIndex: number): number {
  const generation = ++generationSeq;
  pending = [...pending, { generation, targetIndex }];
  return generation;
}

export function endNativeLoad(generation?: number): void {
  pending = generation == null ? [] : pending.filter((p) => p.generation !== generation);
}

export function shouldApplyActiveIndex(index: number): boolean {
  if (pending.length === 0) return true;

  const isPrimingTransient = index === 0 && pending.some((p) => p.targetIndex !== 0);
  if (isPrimingTransient) return false;

  pending = [];
  return true;
}

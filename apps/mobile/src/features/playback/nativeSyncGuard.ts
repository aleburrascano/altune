let pendingTargetIndex: number | null = null;

export function beginNativeLoad(targetIndex: number): void {
  pendingTargetIndex = targetIndex;
}

export function endNativeLoad(): void {
  pendingTargetIndex = null;
}

export function shouldApplyActiveIndex(index: number): boolean {
  if (pendingTargetIndex === null) return true;

  const isPrimingTransient = index === 0 && pendingTargetIndex !== 0;
  if (isPrimingTransient) return false;

  pendingTargetIndex = null;
  return true;
}

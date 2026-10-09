let sessionEpoch = 0;

export function currentSessionEpoch(): number {
  return sessionEpoch;
}

export function isSameSession(epoch: number | undefined): boolean {
  return epoch === sessionEpoch;
}

export function bumpSessionEpoch(): void {
  sessionEpoch += 1;
}

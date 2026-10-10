const MS_PER_SECOND = 1000;

export function progressSecondsToMs(seconds: number): number {
  return seconds * MS_PER_SECOND;
}

export function progressSecondsToRoundedMs(seconds: number): number {
  return Math.round(progressSecondsToMs(seconds));
}

export function secondsToMs(seconds: number): number {
  return Number.isFinite(seconds) ? progressSecondsToRoundedMs(seconds) : 0;
}

export function msToSeconds(ms: number): number {
  return ms / MS_PER_SECOND;
}

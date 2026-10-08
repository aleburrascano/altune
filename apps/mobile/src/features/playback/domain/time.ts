const MS_PER_SECOND = 1000;

export function progressSecondsToMs(seconds: number): number {
  return seconds * MS_PER_SECOND;
}

export function progressSecondsToRoundedMs(seconds: number): number {
  return Math.round(progressSecondsToMs(seconds));
}

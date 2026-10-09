export function formatDuration(totalSeconds: number): string {
  const whole = Math.max(0, Math.floor(totalSeconds));
  const minutes = Math.floor(whole / 60);
  const seconds = whole % 60;
  return `${minutes}:${String(seconds).padStart(2, '0')}`;
}

export function countLabel(n: number, singular: string, plural = `${singular}s`): string {
  return n === 1 ? singular : plural;
}

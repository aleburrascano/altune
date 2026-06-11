export function canPlay(acquisitionStatus: string | undefined | null): boolean {
  return acquisitionStatus === 'ready';
}

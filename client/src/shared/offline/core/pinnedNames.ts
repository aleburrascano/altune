const UNFINISHED_SUFFIX = '.tmp';

export function baseName(uri: string): string {
  return uri.split('/').pop() ?? '';
}

export function trackIdOfUri(uri: string): string | null {
  const [trackId, extension, ...beyondOneExtension] = baseName(uri).split('.');
  const isTrackIdWithOneExtension = extension !== undefined && beyondOneExtension.length === 0;
  return isTrackIdWithOneExtension ? (trackId ?? null) : null;
}

export function unfinishedName(pinnedName: string): string {
  return `${pinnedName}${UNFINISHED_SUFFIX}`;
}

export function isUnfinishedUri(uri: string): boolean {
  return baseName(uri).endsWith(UNFINISHED_SUFFIX);
}

export function unsignedUrl(url: string | undefined): string | undefined {
  return url?.split('?')[0];
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB'];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`;
}

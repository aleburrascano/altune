import { Directory, File, Paths } from 'expo-file-system';

const PINNED_SUBDIR = 'offline-audio';

export function pinnedDir(): Directory {
  const dir = new Directory(Paths.document, PINNED_SUBDIR);
  if (!dir.exists) dir.create({ intermediates: true });
  return dir;
}

function baseName(uri: string): string {
  return uri.split('/').pop() ?? '';
}

export function extFromUrl(url: string): string {
  const path = url.split('?')[0] ?? '';
  const slash = path.lastIndexOf('/');
  const dot = path.lastIndexOf('.');
  return dot > slash && dot < path.length - 1 ? path.slice(dot) : '.mp3';
}

function pinnedFilesOnDisk(): readonly File[] {
  try {
    return pinnedDir()
      .list()
      .filter((entry): entry is File => entry instanceof File);
  } catch {
    return [];
  }
}

export function pinnedDirReadable(): boolean {
  try {
    pinnedDir().list();
    return true;
  } catch {
    return false;
  }
}

export function findPinned(trackId: string): File | null {
  for (const file of pinnedFilesOnDisk()) {
    if (baseName(file.uri).startsWith(`${trackId}.`)) return file;
  }
  return null;
}

function deleteIgnoringFailure(file: File): void {
  try {
    file.delete();
  } catch {}
}

export function deletePinned(trackId: string): void {
  const file = findPinned(trackId);
  if (file === null) return;
  deleteIgnoringFailure(file);
}

export function deleteAllPinned(): void {
  for (const file of pinnedFilesOnDisk()) deleteIgnoringFailure(file);
}

export function pinnedBytes(): number {
  let total = 0;
  for (const file of pinnedFilesOnDisk()) total += file.size ?? 0;
  return total;
}

export async function downloadPinned(trackId: string, url: string): Promise<string> {
  const dest = new File(pinnedDir(), `${trackId}${extFromUrl(url)}`);
  const file = await File.downloadFileAsync(url, dest, { idempotent: true });
  return file.uri;
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

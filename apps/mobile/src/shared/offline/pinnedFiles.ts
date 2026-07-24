/**
 * The on-disk half of offline downloads.
 *
 * Deliberately separate from `features/playback/audioPrefetch.ts`, which looks
 * similar and is not the same thing:
 *
 *   prefetch  → `Paths.cache`,    evicted on a 4-track window, the OS may purge
 *               it under storage pressure. An optimisation.
 *   pinned    → `Paths.document`, deleted only when the user unpins. A promise:
 *               "this plays on a plane".
 *
 * Putting pinned audio in the cache directory would let iOS reclaim it silently,
 * which is exactly the failure this feature exists to prevent — so the two live
 * in different roots and never share an eviction policy.
 */
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

/** Extension of the object behind a (presigned) URL, so the local file keeps the
 *  container hint the native decoder uses. Falls back to .mp3. */
export function extFromUrl(url: string): string {
  const path = url.split('?')[0] ?? '';
  const slash = path.lastIndexOf('/');
  const dot = path.lastIndexOf('.');
  return dot > slash ? path.slice(dot) : '.mp3';
}

/** The pinned file for a track, or null. Matched by `<trackId>.` prefix because
 *  the extension depends on what the provider served. */
export function findPinned(trackId: string): File | null {
  try {
    for (const entry of pinnedDir().list()) {
      if (entry instanceof File && baseName(entry.uri).startsWith(`${trackId}.`)) return entry;
    }
  } catch {
    // directory unreadable — treat as not downloaded
  }
  return null;
}

export function deletePinned(trackId: string): void {
  const file = findPinned(trackId);
  if (file === null) return;
  try {
    file.delete();
  } catch {
    // already gone / locked — the store entry is dropped either way
  }
}

export function deleteAllPinned(): void {
  try {
    for (const entry of pinnedDir().list()) {
      if (entry instanceof File) entry.delete();
    }
  } catch {
    // best-effort
  }
}

/** Total bytes held by pinned downloads — the number Settings shows. */
export function pinnedBytes(): number {
  let total = 0;
  try {
    for (const entry of pinnedDir().list()) {
      if (entry instanceof File) total += entry.size ?? 0;
    }
  } catch {
    return 0;
  }
  return total;
}

/** Download `url` to the pinned directory for `trackId`, returning its file URI. */
export async function downloadPinned(trackId: string, url: string): Promise<string> {
  const dest = new File(pinnedDir(), `${trackId}${extFromUrl(url)}`);
  const file = await File.downloadFileAsync(url, dest, { idempotent: true });
  return file.uri;
}

/** "1.2 GB" / "340 MB" / "12 KB". */
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

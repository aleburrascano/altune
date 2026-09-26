import { downloadStats, downloadUsage } from '../downloadStatsModel';
import { buildDangerZoneActions, type ClearHistoryState } from '../dangerZoneActions';

type PinnedEntry = Parameters<typeof downloadStats>[0][string];

const entry = (trackId: string, status: PinnedEntry['status']): PinnedEntry =>
  ({ trackId, status }) as PinnedEntry;

const clearHistory: ClearHistoryState = {
  mutate: jest.fn(),
  isPending: false,
  isError: false,
  isSuccess: false,
  error: undefined,
};

function removeDownloadsRow(count: number, bytes: number, size: string) {
  const [downloads] = buildDangerZoneActions({
    downloads: {
      stats: { downloadCount: count, downloadBytes: bytes, downloadSize: size, usage: downloadUsage(count, bytes), usageLabel: '', usageDetail: undefined },
      unpinAll: jest.fn(),
    },
    signOutState: { status: 'idle' },
    clearHistory,
    signOut: jest.fn(),
  });
  return downloads;
}

describe('downloadUsage', () => {
  it('is none with no ready track and no bytes on disk', () => {
    expect(downloadUsage(0, 0)).toBe('none');
  });

  it('is leftover with no ready track but bytes on disk', () => {
    expect(downloadUsage(0, 2048)).toBe('leftover');
  });

  it('is tracks once at least one track is ready', () => {
    expect(downloadUsage(1, 2048)).toBe('tracks');
  });
});

describe('downloadStatsModel pins today\'s downloadStats and dangerZoneActions behaviour', () => {
  it('none: hides the size detail and the remove row', () => {
    const stats = downloadStats({ a: entry('a', 'queued') }, 0);
    expect(stats.usage).toBe('none');
    expect(stats.usageLabel).toBe('No downloads on this device');
    expect(stats.usageDetail).toBeUndefined();

    const row = removeDownloadsRow(stats.downloadCount, stats.downloadBytes, stats.downloadSize);
    expect(row?.row.hidden).toBe(true);
  });

  it('none: the confirm body still reads as the leftover wording, matching the pre-refactor copy', () => {
    const stats = downloadStats({ a: entry('a', 'queued') }, 0);
    const row = removeDownloadsRow(stats.downloadCount, stats.downloadBytes, stats.downloadSize);

    expect(row?.confirm.body).toBe('Leftover download files (0 B) will be deleted from this device.');
  });

  it('leftover: shows the leftover label and keeps the remove row with the leftover confirm body', () => {
    const stats = downloadStats({}, 2048);
    expect(stats.usage).toBe('leftover');
    expect(stats.usageLabel).toBe('Leftover download files');
    expect(stats.usageDetail).toBe(stats.downloadSize);

    const row = removeDownloadsRow(stats.downloadCount, stats.downloadBytes, stats.downloadSize);
    expect(row?.row.hidden).toBe(false);
    expect(row?.confirm.body).toBe(
      `Leftover download files (${stats.downloadSize}) will be deleted from this device.`,
    );
  });

  it('tracks: shows the track-count label and the tracks confirm body', () => {
    const stats = downloadStats({ a: entry('a', 'ready'), b: entry('b', 'ready') }, 5 * 1024 ** 2);
    expect(stats.usage).toBe('tracks');
    expect(stats.usageLabel).toBe('2 tracks');
    expect(stats.usageDetail).toBe(stats.downloadSize);

    const row = removeDownloadsRow(stats.downloadCount, stats.downloadBytes, stats.downloadSize);
    expect(row?.row.hidden).toBe(false);
    expect(row?.confirm.body).toBe(
      `2 tracks (${stats.downloadSize}) will be deleted from this device. They stay in your library and can be downloaded again.`,
    );
  });
});

describe('download usage at the count and byte boundaries', () => {
  const readyEntries = (n: number) =>
    Object.fromEntries(
      Array.from({ length: n }, (_, i) => [`t${i}`, entry(`t${i}`, 'ready')]),
    ) as Parameters<typeof downloadStats>[0];

  it.each([
    { count: 0, bytes: 0, usage: 'none', label: 'No downloads on this device', hidden: true },
    { count: 0, bytes: 1, usage: 'leftover', label: 'Leftover download files', hidden: false },
    { count: 1, bytes: 0, usage: 'tracks', label: '1 track', hidden: false },
    { count: 1, bytes: 1, usage: 'tracks', label: '1 track', hidden: false },
    { count: 3, bytes: 4096, usage: 'tracks', label: '3 tracks', hidden: false },
  ] as const)(
    '$count ready / $bytes bytes reads as $usage in the label, detail and remove row alike',
    ({ count, bytes, usage, label, hidden }) => {
      const stats = downloadStats(readyEntries(count), bytes);

      expect(downloadUsage(count, bytes)).toBe(usage);
      expect(stats.usage).toBe(usage);
      expect(stats.usageLabel).toBe(label);
      expect(stats.usageDetail).toBe(usage === 'none' ? undefined : stats.downloadSize);
      const row = removeDownloadsRow(stats.downloadCount, stats.downloadBytes, stats.downloadSize);
      expect(row?.row.hidden).toBe(hidden);
    },
  );

  it('an empty index with nothing on disk hides the remove row', () => {
    const stats = downloadStats({}, 0);

    expect(stats.usage).toBe('none');
    expect(stats.usageDetail).toBeUndefined();
    expect(removeDownloadsRow(0, 0, stats.downloadSize)?.row.hidden).toBe(true);
  });

  it('a ready track whose bytes measure zero still offers removal with its track count', () => {
    const stats = downloadStats({ a: entry('a', 'ready') }, 0);

    expect(stats.usageDetail).toBe('0 B');
    const row = removeDownloadsRow(stats.downloadCount, stats.downloadBytes, stats.downloadSize);
    expect(row?.row.hidden).toBe(false);
    expect(row?.confirm.body.startsWith('1 track (0 B) will be deleted from this device.')).toBe(
      true,
    );
  });

  it('a single leftover byte keeps the remove row with the leftover confirm body', () => {
    const stats = downloadStats({}, 1);

    expect(stats.usageDetail).toBe('1 B');
    const row = removeDownloadsRow(stats.downloadCount, stats.downloadBytes, stats.downloadSize);
    expect(row?.row.hidden).toBe(false);
    expect(row?.confirm.body).toBe('Leftover download files (1 B) will be deleted from this device.');
  });

  it('bytes left by entries that never became ready read as leftover, not tracks', () => {
    const stats = downloadStats(
      { a: entry('a', 'queued'), b: entry('b', 'downloading'), c: entry('c', 'failed') },
      4096,
    );

    expect(stats.usage).toBe('leftover');
    expect(stats.usageLabel).toBe('Leftover download files');
    const row = removeDownloadsRow(stats.downloadCount, stats.downloadBytes, stats.downloadSize);
    expect(row?.confirm.body).toBe(
      'Leftover download files (4.0 KB) will be deleted from this device.',
    );
  });

  it('a large library reads as tracks with the full count in label and confirm body', () => {
    const stats = downloadStats(readyEntries(250), 5 * 1024 ** 2);

    expect(stats.usage).toBe('tracks');
    expect(stats.usageLabel).toBe('250 tracks');
    expect(stats.usageDetail).toBe('5.0 MB');
    const row = removeDownloadsRow(stats.downloadCount, stats.downloadBytes, stats.downloadSize);
    expect(row?.confirm.body).toBe(
      '250 tracks (5.0 MB) will be deleted from this device. They stay in your library and can be downloaded again.',
    );
  });

  it('a very large leftover total with no ready track stays leftover', () => {
    expect(downloadUsage(0, Number.MAX_SAFE_INTEGER)).toBe('leftover');
  });
});

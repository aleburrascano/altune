import { downloadStats, downloadUsage } from '../downloadStatsModel';
import { buildDangerZoneActions } from '../ui/dangerZoneActions';

type PinnedEntry = Parameters<typeof downloadStats>[0][string];

const entry = (trackId: string, status: PinnedEntry['status']): PinnedEntry =>
  ({ trackId, status }) as PinnedEntry;

function removeDownloadsRow(count: number, bytes: number, size: string) {
  const [downloads] = buildDangerZoneActions({
    downloadCount: count,
    downloadBytes: bytes,
    downloadSize: size,
    signOutState: { status: 'idle' },
    clearHistory: {} as Parameters<typeof buildDangerZoneActions>[0]['clearHistory'],
    unpinAll: jest.fn(),
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

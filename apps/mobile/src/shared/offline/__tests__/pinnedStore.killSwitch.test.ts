// Regression for issue #955: the pinned-download worker must stop while its remote kill switch is
// off, so a download loop thrashing disk can be stopped without an app release.

import { act } from '@testing-library/react-native';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import { applyKillSwitches, setKillSwitchFileStore } from '@shared/killSwitch/killSwitch';

import { usePinnedStore } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const fetchAudioUrlsMock = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

function resolved(trackId: string): ResolvedAudioUrl {
  return { trackId, url: `https://cdn.example.com/audio/${trackId}.mp3`, version: 'v1' };
}

async function flush(rounds = 30): Promise<void> {
  for (let i = 0; i < rounds; i += 1) await Promise.resolve();
}

function statuses(): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [id, entry] of Object.entries(usePinnedStore.getState().entries))
    out[id] = entry.status;
  return out;
}

beforeEach(() => {
  setKillSwitchFileStore(createMemoryFileStore());
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  fetchAudioUrlsMock.mockReset().mockImplementation(async (ids) => ids.map(resolved));
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  setKillSwitchFileStore();
  jest.restoreAllMocks();
});

describe('pinned downloads — remote kill switch', () => {
  it('downloads nothing while the switch is off and leaves the tracks queued', async () => {
    applyKillSwitches({ offline_downloads_enabled: false });

    await act(async () => {
      usePinnedStore.getState().pin(asTrackId('A'));
      void usePinnedStore.getState().pinMany([asTrackId('B')]);
      await flush();
    });

    expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
    expect(statuses()).toEqual({ A: 'queued', B: 'queued' });
    expect(usePinnedStore.getState().queue).toEqual(['A', 'B']);
    expect(usePinnedStore.getState().isWorking).toBe(false);
  });

  it('does not resume queued tracks on reconcile while the switch is off', async () => {
    applyKillSwitches({ offline_downloads_enabled: false });
    usePinnedStore.setState({ entries: { A: { trackId: asTrackId('A'), status: 'queued' } } });

    await act(async () => {
      usePinnedStore.getState().reconcile();
      await flush();
    });

    expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
    expect(statuses()).toEqual({ A: 'queued' });
  });

  it('stops the drain before the next track when the switch is turned off mid-batch', async () => {
    let releaseFirst!: () => void;
    fetchAudioUrlsMock.mockImplementationOnce(
      (ids) =>
        new Promise((resolve) => {
          releaseFirst = () => resolve(ids.map(resolved));
        }),
    );

    await act(async () => {
      void usePinnedStore.getState().pinMany([asTrackId('A'), asTrackId('B'), asTrackId('C')]);
      await flush();
    });
    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);

    await act(async () => {
      applyKillSwitches({ offline_downloads_enabled: false });
      releaseFirst();
      await flush();
    });

    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);
    expect(statuses()).toEqual({ A: 'ready', B: 'queued', C: 'queued' });
    expect(usePinnedStore.getState().queue).toEqual(['B', 'C']);
  });

  it('resumes the held-back queue when the switch is turned back on', async () => {
    applyKillSwitches({ offline_downloads_enabled: false });
    let batch!: Promise<unknown>;
    await act(async () => {
      batch = usePinnedStore.getState().pinMany([asTrackId('A'), asTrackId('B')]);
      await flush();
    });

    await act(async () => {
      applyKillSwitches({ offline_downloads_enabled: true });
      await flush(60);
    });

    await expect(batch).resolves.toEqual({ requested: 2, failed: 0 });
    expect(fetchAudioUrlsMock.mock.calls.map(([ids]) => ids)).toEqual([['A'], ['B']]);
    expect(statuses()).toEqual({ A: 'ready', B: 'ready' });
  });

  it('has nothing to resume when the switch comes back on with an empty queue', () => {
    applyKillSwitches({ offline_downloads_enabled: false });

    applyKillSwitches({ offline_downloads_enabled: true });

    expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
    expect(usePinnedStore.getState().isWorking).toBe(false);
  });
});

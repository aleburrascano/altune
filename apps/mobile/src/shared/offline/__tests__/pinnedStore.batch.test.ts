import { act } from '@testing-library/react-native';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';

import { usePinnedStore } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const fetchAudioUrlsMock = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

function resolved(trackId: string): ResolvedAudioUrl {
  return { trackId, url: `https://cdn.example.com/audio/${trackId}.mp3?sig=t&exp=999`, version: 'v1' };
}

async function flush(rounds = 40): Promise<void> {
  for (let i = 0; i < rounds; i += 1) {
    await Promise.resolve();
  }
}

beforeEach(() => {
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  fetchAudioUrlsMock.mockReset();
});

describe('pinMany — per-batch result', () => {
  it('resolves once every track in the batch settles, counting the failed ones', async () => {
    fetchAudioUrlsMock.mockImplementation(async ([id]) => {
      if (id === 'B') throw new Error('404');
      return [resolved(id!)];
    });

    let result: unknown;
    await act(async () => {
      result = await usePinnedStore
        .getState()
        .pinMany([asTrackId('A'), asTrackId('B'), asTrackId('C')]);
    });

    expect(result).toEqual({ requested: 3, failed: 1 });
    expect(usePinnedStore.getState().entries['B']?.status).toBe('failed');
  });

  it('stays pending while a batch track is still downloading', async () => {
    let finish!: (urls: ResolvedAudioUrl[]) => void;
    fetchAudioUrlsMock.mockImplementation(
      () => new Promise<ResolvedAudioUrl[]>((res) => (finish = res)),
    );

    let settled = false;
    const batch = usePinnedStore.getState().pinMany([asTrackId('A')]);
    void batch.then(() => (settled = true));
    await act(async () => flush());
    expect(settled).toBe(false);

    await act(async () => {
      finish([resolved('A')]);
      await flush();
    });
    expect(settled).toBe(true);
    await expect(batch).resolves.toEqual({ requested: 1, failed: 0 });
  });

  it('only counts tracks the batch actually queued, and a track unpinned mid-batch is not a failure', async () => {
    usePinnedStore.setState({
      entries: { A: { trackId: asTrackId('A'), status: 'ready', uri: 'file:///a', version: 'v1' } },
    });
    let finish!: (urls: ResolvedAudioUrl[]) => void;
    fetchAudioUrlsMock.mockImplementation(
      () => new Promise<ResolvedAudioUrl[]>((res) => (finish = res)),
    );

    const batch = usePinnedStore.getState().pinMany([asTrackId('A'), asTrackId('B')]);
    await act(async () => {
      usePinnedStore.getState().unpin(asTrackId('B'));
      finish([resolved('B')]);
      await flush();
    });

    await expect(batch).resolves.toEqual({ requested: 1, failed: 0 });
  });

  it('resolves an empty result straight away when nothing needs downloading', async () => {
    await expect(usePinnedStore.getState().pinMany([])).resolves.toEqual({
      requested: 0,
      failed: 0,
    });
  });
});

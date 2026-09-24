import { act } from '@testing-library/react-native';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

import { setPinnedFileStore } from '../pinnedFiles';
import { resolvePinnedUri, usePinnedStore } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const fetchAudioUrlsMock = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

const AUDIO_DIR = 'memory://document/offline-audio';
const T1 = asTrackId('t1');

function resolved(trackId: string): ResolvedAudioUrl {
  return { trackId, url: `https://cdn.example.com/audio/${trackId}.mp3?sig=secret`, version: 'v1' };
}

function filesInAudioDir(store: MemoryFileStore): string[] {
  return [...store.files.keys()].filter((uri) => uri.startsWith(`${AUDIO_DIR}/`));
}

async function flush(rounds = 40): Promise<void> {
  for (let i = 0; i < rounds; i += 1) {
    await Promise.resolve();
  }
}

async function killMidDownloadThenRelaunch(store: MemoryFileStore): Promise<void> {
  store.download = (_url, dest) => {
    dest.write('first-few-bytes');
    return new Promise<string>(() => {});
  };
  usePinnedStore.getState().pin(T1);
  await act(async () => flush());
  expect(usePinnedStore.getState().entries['t1']?.status).toBe('downloading');
  fetchAudioUrlsMock.mockImplementation(() => new Promise<ResolvedAudioUrl[]>(() => {}));
  usePinnedStore.setState({ queue: [], isWorking: false });
}

let store: MemoryFileStore;

beforeEach(() => {
  jest.useFakeTimers();
  store = createMemoryFileStore();
  setPinnedFileStore(store);
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  fetchAudioUrlsMock.mockReset();
  fetchAudioUrlsMock.mockImplementation(async ([id]) => [resolved(id!)]);
});

afterEach(() => {
  setPinnedFileStore();
  jest.useRealTimers();
});

describe('a pinned download cut off by an app kill', () => {
  it('is downloaded again on the next launch instead of being served as a ready offline copy', async () => {
    await killMidDownloadThenRelaunch(store);

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries['t1']).toEqual({ trackId: T1, status: 'downloading' });
    expect(resolvePinnedUri(T1)).toBeUndefined();
  });

  it('leaves no partial bytes behind once the next launch has reconciled', async () => {
    await killMidDownloadThenRelaunch(store);

    usePinnedStore.getState().reconcile();

    expect(filesInAudioDir(store)).toEqual([]);
  });
});

describe('reconcile with an unfinished download file on disk', () => {
  it.each(['queued', 'downloading'] as const)(
    'does not adopt the unfinished file of a %s entry as ready',
    (status) => {
      store.files.set(`${AUDIO_DIR}/t1.mp3.tmp`, 'first-few-bytes');
      usePinnedStore.setState({ entries: { t1: { trackId: T1, status } }, isWorking: true });

      usePinnedStore.getState().reconcile();

      expect(usePinnedStore.getState().entries['t1']).toEqual({ trackId: T1, status: 'queued' });
    },
  );

  it('keeps the unfinished file while a drain is running, since it may be the transfer in flight', () => {
    store.files.set(`${AUDIO_DIR}/t1.mp3.tmp`, 'first-few-bytes');
    usePinnedStore.setState({ entries: { t1: { trackId: T1, status: 'downloading' } }, isWorking: true });

    usePinnedStore.getState().reconcile();

    expect(filesInAudioDir(store)).toEqual([`${AUDIO_DIR}/t1.mp3.tmp`]);
  });

  it('still adopts a finished download whose ready status never reached the index', () => {
    store.files.set(`${AUDIO_DIR}/t1.mp3`, 'whole-track');
    usePinnedStore.setState({ entries: { t1: { trackId: T1, status: 'downloading' } }, isWorking: true });

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries['t1']).toEqual({
      trackId: T1,
      status: 'ready',
      uri: `${AUDIO_DIR}/t1.mp3`,
    });
  });
});

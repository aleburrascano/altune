import * as FileSystem from 'expo-file-system';

import { fetchAudioUrls } from '@shared/api-client/audio';

import { runDownloadQueue } from '../pinnedDownloadWorker';
import type { PinnedEntry } from '../pinnedIndex';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const fetchAudioUrlsMock = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

const { __fs } = FileSystem as unknown as {
  __fs: { reset(): void; readFile(uri: string): string | undefined };
};

const INDEX_URI = 'file:///document/offline/pinned.json';

type State = { entries: Record<string, PinnedEntry>; queue: string[]; isWorking: boolean };
type Update = Partial<State> | ((s: State) => Partial<State>);

beforeEach(() => {
  __fs.reset();
  fetchAudioUrlsMock.mockReset();
});

describe('runDownloadQueue', () => {
  it('never re-creates an entry that is removed between the worker reading and writing it', async () => {
    let state: State = {
      entries: { t1: { trackId: 't1', status: 'queued' } },
      queue: ['t1'],
      isWorking: false,
    };
    const get = (): State => state;
    let updaters = 0;
    // Simulates an unpin landing after the worker checked the entry but before its
    // 'downloading' mark (the second functional update, after the dequeue) applies.
    const set = (update: Update): void => {
      if (typeof update !== 'function') {
        state = { ...state, ...update };
        return;
      }
      updaters += 1;
      if (updaters === 2) state = { ...state, entries: {} };
      state = { ...state, ...update(state) };
    };
    fetchAudioUrlsMock.mockResolvedValue([]);

    await runDownloadQueue(set, get);

    expect(state.entries).toEqual({});
    expect(state.queue).toEqual([]);
    expect(state.isWorking).toBe(false);
    expect(__fs.readFile(INDEX_URI)).toBeUndefined();
  });
});

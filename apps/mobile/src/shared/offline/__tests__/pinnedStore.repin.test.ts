import { act } from '@testing-library/react-native';
import * as FileSystem from 'expo-file-system';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';

import { repinIfPinned, usePinnedStore } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const fetchAudioUrlsMock = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

type FsFailureKind = 'write' | 'read' | 'delete' | 'download' | 'createDirectory';

const { __fs } = FileSystem as unknown as {
  __fs: {
    reset(): void;
    seedDirectory(uri: string): void;
    seedFile(uri: string, contents: string): void;
    readFile(uri: string): string | undefined;
    allFiles(): Record<string, string>;
    failNext(kind: FsFailureKind, error?: Error): void;
  };
};

const PINNED_DIR_URI = 'file:///document/offline-audio';

function pinnedUri(name: string): string {
  return `${PINNED_DIR_URI}/${name}`;
}

function signedUrl(trackId: string, generation = 1): string {
  return `https://cdn.example.com/audio/${trackId}.mp3?sig=token-${generation}&exp=999`;
}

function resolved(trackId: string, generation = 1): ResolvedAudioUrl {
  return { trackId, url: signedUrl(trackId, generation), version: `v${generation}` };
}

type PendingCall = {
  ids: readonly string[];
  resolve: (urls: ResolvedAudioUrl[]) => void;
  reject: (err: unknown) => void;
};

function captureAudioUrlCalls(): PendingCall[] {
  const calls: PendingCall[] = [];
  fetchAudioUrlsMock.mockImplementation((ids: string[]) => {
    let resolve!: PendingCall['resolve'];
    let reject!: PendingCall['reject'];
    const promise = new Promise<ResolvedAudioUrl[]>((res, rej) => {
      resolve = res;
      reject = rej;
    });
    calls.push({ ids, resolve, reject });
    return promise;
  });
  return calls;
}

async function flush(rounds = 20): Promise<void> {
  for (let i = 0; i < rounds; i += 1) {
    await Promise.resolve();
  }
}

async function drainWorker(calls: PendingCall[], nextIndex: number): Promise<string[]> {
  const usedUrls: string[] = [];
  let i = nextIndex;
  while (usePinnedStore.getState().isWorking) {
    const call = calls[i];
    if (call === undefined) {
      throw new Error('worker still marked isWorking with no pending fetchAudioUrls call left to resolve');
    }
    const urls = call.ids.map((id) => resolved(id, i + 1));
    call.resolve(urls);
    usedUrls.push(urls[0]?.url ?? '');
    i += 1;
    await flush();
  }
  return usedUrls;
}

beforeEach(() => {
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  fetchAudioUrlsMock.mockReset();
});

describe('repinIfPinned — guard arm', () => {
  it('does nothing for a track that was never pinned: no url resolution is issued and no entry is created', () => {
    const calls = captureAudioUrlCalls();

    repinIfPinned('never-pinned');

    expect(calls).toHaveLength(0);
    expect(usePinnedStore.getState().entries['never-pinned']).toBeUndefined();
    expect(usePinnedStore.getState().queue).toEqual([]);
  });
});

describe('repinIfPinned — replace arm', () => {
  it('re-downloads a previously-downloaded track under the same key, ending with the new audio instead of the old', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    await act(async () => {
      calls[0]?.resolve([resolved('A', 1)]);
      await flush();
    });
    expect(usePinnedStore.getState().entries['A']?.status).toBe('ready');
    expect(__fs.readFile(pinnedUri('A.mp3'))).toBe(`downloaded:${signedUrl('A', 1)}`);

    repinIfPinned('A');
    expect(__fs.readFile(pinnedUri('A.mp3'))).toBeUndefined();
    expect(usePinnedStore.getState().entries['A']?.status).toBe('downloading');
    expect(calls).toHaveLength(2);

    await act(async () => {
      calls[1]?.resolve([resolved('A', 2)]);
      await flush();
    });

    expect(usePinnedStore.getState().entries['A']).toEqual({
      trackId: 'A',
      status: 'ready',
      uri: pinnedUri('A.mp3'),
      version: 'v2',
    });
    expect(__fs.readFile(pinnedUri('A.mp3'))).toBe(`downloaded:${signedUrl('A', 2)}`);
  });

  it('replaying repinIfPinned twice in a row for the same track leaves exactly one entry and one queue slot once the worker settles', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    await act(async () => {
      calls[0]?.resolve([resolved('A', 1)]);
      await flush();
    });

    repinIfPinned('A');
    repinIfPinned('A');
    expect(calls).toHaveLength(2);
    expect(usePinnedStore.getState().queue).toEqual(['A']);

    const usedUrls = await drainWorker(calls, 1);

    expect(Object.keys(usePinnedStore.getState().entries)).toEqual(['A']);
    expect(usePinnedStore.getState().queue).toEqual([]);
    expect(usePinnedStore.getState().entries['A']?.status).toBe('ready');
    const lastUrl = usedUrls[usedUrls.length - 1];
    expect(__fs.readFile(pinnedUri('A.mp3'))).toBe(`downloaded:${lastUrl}`);
  });
});

describe('repinIfPinned firing while the track is currently downloading', () => {
  it('joins the running worker instead of starting a second live download, and the final ready entry holds the NEW audio', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    expect(usePinnedStore.getState().entries['A']?.status).toBe('downloading');
    expect(calls).toHaveLength(1);

    repinIfPinned('A');

    expect(calls).toHaveLength(1);
    expect(usePinnedStore.getState().entries['A']?.status).toBe('queued');
    expect(usePinnedStore.getState().queue).toEqual(['A']);

    await act(async () => {
      calls[0]?.resolve([resolved('A', 1)]);
      await flush();
    });

    expect(calls).toHaveLength(2);
    expect(calls[1]?.ids).toEqual(['A']);
    expect(usePinnedStore.getState().entries['A']?.status).toBe('downloading');

    await act(async () => {
      calls[1]?.resolve([resolved('A', 2)]);
      await flush();
    });

    expect(usePinnedStore.getState().entries['A']).toEqual({
      trackId: 'A',
      status: 'ready',
      uri: pinnedUri('A.mp3'),
      version: 'v2',
    });
    expect(__fs.readFile(pinnedUri('A.mp3'))).toBe(`downloaded:${signedUrl('A', 2)}`);
    expect(usePinnedStore.getState().isWorking).toBe(false);
    expect(usePinnedStore.getState().queue).toEqual([]);
  });

  it('never publishes the superseded download as ready, and never leaves its bytes on disk, even transiently while the fresh download is still queued behind it', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    repinIfPinned('A');
    expect(usePinnedStore.getState().entries['A']).toEqual({ trackId: 'A', status: 'queued' });

    let sawStaleReady = false;
    await act(async () => {
      calls[0]?.resolve([resolved('A', 1)]);
      for (let i = 0; i < 60; i += 1) {
        await Promise.resolve();
        const entry = usePinnedStore.getState().entries['A'];
        if (entry?.status === 'ready' && entry.uri === pinnedUri('A.mp3')) sawStaleReady = true;
      }
    });

    expect(sawStaleReady).toBe(false);
    expect(__fs.readFile(pinnedUri('A.mp3'))).toBeUndefined();
  });

  it('performs no status write at all for the superseded download: the entry stays exactly what the re-pin left it as until the fresh download reaches its own worker turn', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    repinIfPinned('A');
    const afterRepin = usePinnedStore.getState().entries['A'];

    let sawUnexpectedWrite = false;
    await act(async () => {
      calls[0]?.resolve([resolved('A', 1)]);
      for (let i = 0; i < 60; i += 1) {
        await Promise.resolve();
        const status = usePinnedStore.getState().entries['A']?.status;
        if (status !== undefined && status !== afterRepin?.status && status !== 'downloading') {
          sawUnexpectedWrite = true;
        }
      }
    });

    expect(sawUnexpectedWrite).toBe(false);
  });
});

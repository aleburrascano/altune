import { act } from '@testing-library/react-native';
import * as FileSystem from 'expo-file-system';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';

import { usePinnedStore } from '../pinnedStore';

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
const INDEX_URI = 'file:///document/offline/pinned.json';

function pinnedUri(name: string): string {
  return `${PINNED_DIR_URI}/${name}`;
}

function signedUrl(trackId: string, generation = 1): string {
  return `https://cdn.example.com/audio/${trackId}.mp3?sig=token-${generation}&exp=999`;
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

beforeEach(() => {
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  fetchAudioUrlsMock.mockReset();
});

describe('runQueue — isWorking reentrancy guard', () => {
  it('a second pin() arriving while the first track is still downloading does not issue a second concurrent url resolution', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    expect(calls).toHaveLength(1);
    expect(calls[0]?.ids).toEqual(['A']);

    usePinnedStore.getState().pin('B');
    expect(calls).toHaveLength(1);
    expect(usePinnedStore.getState().entries['B']?.status).toBe('queued');

    await act(async () => {
      calls[0]?.resolve([{ trackId: 'A', url: signedUrl('A') }]);
      await flush();
    });

    expect(calls).toHaveLength(2);
    expect(calls[1]?.ids).toEqual(['B']);
    expect(usePinnedStore.getState().entries['A']?.status).toBe('ready');

    await act(async () => {
      calls[1]?.resolve([{ trackId: 'B', url: signedUrl('B') }]);
      await flush();
    });

    expect(usePinnedStore.getState().entries['B']?.status).toBe('ready');
    expect(usePinnedStore.getState().isWorking).toBe(false);
    expect(usePinnedStore.getState().queue).toEqual([]);
  });

  it('pinMany() arriving mid-download queues its tracks behind the running worker instead of starting concurrent downloads', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    expect(calls).toHaveLength(1);

    usePinnedStore.getState().pinMany(['B', 'C']);
    expect(calls).toHaveLength(1);

    await act(async () => {
      calls[0]?.resolve([{ trackId: 'A', url: signedUrl('A') }]);
      await flush();
    });
    expect(calls).toHaveLength(2);
    expect(calls[1]?.ids).toEqual(['B']);

    await act(async () => {
      calls[1]?.resolve([{ trackId: 'B', url: signedUrl('B') }]);
      await flush();
    });
    expect(calls).toHaveLength(3);
    expect(calls[2]?.ids).toEqual(['C']);

    await act(async () => {
      calls[2]?.resolve([{ trackId: 'C', url: signedUrl('C') }]);
      await flush();
    });

    expect(usePinnedStore.getState().entries['A']?.status).toBe('ready');
    expect(usePinnedStore.getState().entries['B']?.status).toBe('ready');
    expect(usePinnedStore.getState().entries['C']?.status).toBe('ready');
    expect(usePinnedStore.getState().isWorking).toBe(false);
  });

  it('unpin() of the currently-downloading track removes it immediately, and the worker is not left wedged once the in-flight transfer settles', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    expect(usePinnedStore.getState().entries['A']?.status).toBe('downloading');

    usePinnedStore.getState().unpin('A');
    expect(usePinnedStore.getState().entries['A']).toBeUndefined();

    await act(async () => {
      calls[0]?.resolve([{ trackId: 'A', url: signedUrl('A') }]);
      await flush();
    });

    expect(usePinnedStore.getState().entries['A']).toBeUndefined();
    expect(__fs.allFiles()[pinnedUri('A.mp3')]).toBeUndefined();
    expect(usePinnedStore.getState().isWorking).toBe(false);
  });

  it('unpinAll() mid-download (sign-out) tears down the in-flight track and drops any tracks still waiting behind it, without ever resolving their urls', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pinMany(['A', 'B']);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.ids).toEqual(['A']);

    usePinnedStore.getState().unpinAll();
    expect(usePinnedStore.getState().entries).toEqual({});
    expect(usePinnedStore.getState().queue).toEqual([]);

    await act(async () => {
      calls[0]?.resolve([{ trackId: 'A', url: signedUrl('A') }]);
      await flush();
    });

    expect(calls).toHaveLength(1);
    expect(usePinnedStore.getState().entries).toEqual({});
    expect(__fs.readFile(pinnedUri('A.mp3'))).toBeUndefined();
    expect(__fs.readFile(pinnedUri('B.mp3'))).toBeUndefined();
    expect(usePinnedStore.getState().isWorking).toBe(false);
  });
});

describe('runQueue — the worker is never wedged after a terminal outcome', () => {
  it('is not left wedged after a failure: a fresh pin right after a failed download starts a new worker immediately', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    await act(async () => {
      calls[0]?.reject(new Error('network drop'));
      await flush();
    });
    expect(usePinnedStore.getState().isWorking).toBe(false);

    usePinnedStore.getState().pin('B');
    expect(usePinnedStore.getState().entries['B']?.status).toBe('downloading');
    expect(calls).toHaveLength(2);
  });
});

describe('downloadOne', () => {
  it('resolves a signed url, writes the file and marks the entry ready with the local uri', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    expect(usePinnedStore.getState().entries['A']?.status).toBe('downloading');

    await act(async () => {
      calls[0]?.resolve([{ trackId: 'A', url: signedUrl('A') }]);
      await flush();
    });

    expect(usePinnedStore.getState().entries['A']).toEqual({
      trackId: 'A',
      status: 'ready',
      uri: pinnedUri('A.mp3'),
    });
    expect(usePinnedStore.getState().isWorking).toBe(false);
  });

  it('a resolved-urls response that omits the requested id (an empty array) lands the entry in failed rather than hanging the worker', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');

    await act(async () => {
      calls[0]?.resolve([]);
      await flush();
    });

    expect(usePinnedStore.getState().entries['A']?.status).toBe('failed');
    expect(usePinnedStore.getState().isWorking).toBe(false);
    expect(usePinnedStore.getState().queue).toEqual([]);
  });

  it('a rejected url resolution (network drop) marks the entry failed', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');

    await act(async () => {
      calls[0]?.reject(new Error('network drop'));
      await flush();
    });

    expect(usePinnedStore.getState().entries['A']?.status).toBe('failed');
    expect(usePinnedStore.getState().isWorking).toBe(false);
  });

  it('a disk failure mid-download (stream disconnect) marks the entry failed instead of leaving it stuck downloading', async () => {
    const calls = captureAudioUrlCalls();
    __fs.failNext('download', new Error('stream disconnected'));

    usePinnedStore.getState().pin('A');

    await act(async () => {
      calls[0]?.resolve([{ trackId: 'A', url: signedUrl('A') }]);
      await flush();
    });

    expect(usePinnedStore.getState().entries['A']?.status).toBe('failed');
    expect(usePinnedStore.getState().isWorking).toBe(false);
  });

  it('a failed track is offered for retry: pinning it again after a failure starts a fresh download rather than staying pending forever', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    await act(async () => {
      calls[0]?.resolve([]);
      await flush();
    });
    expect(usePinnedStore.getState().entries['A']?.status).toBe('failed');

    usePinnedStore.getState().pin('A');
    expect(calls).toHaveLength(2);

    await act(async () => {
      calls[1]?.resolve([{ trackId: 'A', url: signedUrl('A') }]);
      await flush();
    });

    expect(usePinnedStore.getState().entries['A']?.status).toBe('ready');
  });

  it('an index write failure while marking the entry ready leaves in-memory state correct but the persisted index silently stale', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    await act(async () => {
      calls[0]?.resolve([{ trackId: 'A', url: signedUrl('A') }]);
      await flush();
    });

    usePinnedStore.getState().pin('B');
    const beforeFinalWrite = __fs.readFile(INDEX_URI);
    __fs.failNext('write', new Error('disk full'));

    await act(async () => {
      calls[1]?.resolve([{ trackId: 'B', url: signedUrl('B') }]);
      await flush();
    });

    expect(usePinnedStore.getState().entries['B']).toEqual({
      trackId: 'B',
      status: 'ready',
      uri: pinnedUri('B.mp3'),
    });
    expect(__fs.readFile(INDEX_URI)).toBe(beforeFinalWrite);
  });

  it('cleans up the file left behind by an unpin that landed mid-download, once the download succeeds after the unpin', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    usePinnedStore.getState().unpin('A');

    await act(async () => {
      calls[0]?.resolve([{ trackId: 'A', url: signedUrl('A') }]);
      await flush();
    });

    expect(usePinnedStore.getState().entries['A']).toBeUndefined();
    expect(__fs.readFile(pinnedUri('A.mp3'))).toBeUndefined();
  });

  it('also cleans up a stray file at the track path when the download that was unpinned mid-flight instead fails', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    usePinnedStore.getState().unpin('A');
    __fs.seedFile(pinnedUri('A.mp3'), 'partial-bytes-written-during-the-in-flight-attempt');

    await act(async () => {
      calls[0]?.reject(new Error('network drop mid-transfer'));
      await flush();
    });

    expect(usePinnedStore.getState().entries['A']).toBeUndefined();
    expect(__fs.readFile(pinnedUri('A.mp3'))).toBeUndefined();
  });

  it('skips a queued id whose entry was removed before the worker reached it, forced via direct state mutation since the public pin/unpin surface cannot currently produce it', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    expect(calls).toHaveLength(1);
    usePinnedStore.setState((s) => ({ queue: [...s.queue, 'ghost'] }));

    await act(async () => {
      calls[0]?.resolve([{ trackId: 'A', url: signedUrl('A') }]);
      await flush();
    });

    expect(calls).toHaveLength(1);
    expect(usePinnedStore.getState().entries['ghost']).toBeUndefined();
    expect(usePinnedStore.getState().queue).toEqual([]);
    expect(usePinnedStore.getState().isWorking).toBe(false);
  });
});

describe('downloadOne — replaying a pin for an already-ready track', () => {
  it('is a no-op: no new url resolution is issued and the entry is unchanged', async () => {
    const calls = captureAudioUrlCalls();

    usePinnedStore.getState().pin('A');
    await act(async () => {
      calls[0]?.resolve([{ trackId: 'A', url: signedUrl('A') }]);
      await flush();
    });
    const readyEntry = usePinnedStore.getState().entries['A'];
    expect(readyEntry?.status).toBe('ready');

    usePinnedStore.getState().pin('A');

    expect(calls).toHaveLength(1);
    expect(usePinnedStore.getState().entries['A']).toEqual(readyEntry);
    expect(usePinnedStore.getState().queue).toEqual([]);
  });
});

describe('security — the signed download url never reaches disk', () => {
  it('after a successful download, the persisted index and the in-memory entry hold only the local file uri, never the source url or its query string', async () => {
    const calls = captureAudioUrlCalls();
    const url = signedUrl('A', 1);

    usePinnedStore.getState().pin('A');
    await act(async () => {
      calls[0]?.resolve([{ trackId: 'A', url }]);
      await flush();
    });

    const entry = usePinnedStore.getState().entries['A'];
    expect(entry?.uri).toBe(pinnedUri('A.mp3'));
    expect(entry?.uri).not.toContain('?');
    expect(entry?.uri).not.toContain('sig=');

    const persisted = __fs.readFile(INDEX_URI);
    expect(persisted).not.toContain(url);
    expect(persisted).not.toContain('token-1');
    expect(JSON.parse(persisted as string)).toEqual({
      A: { trackId: 'A', status: 'ready', uri: pinnedUri('A.mp3') },
    });
  });
});

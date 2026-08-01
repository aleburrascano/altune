import { act } from '@testing-library/react-native';
import * as FileSystem from 'expo-file-system';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';

import { pinnedUri, usePinnedStore } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const fetchAudioUrlsMock = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

const { __fs } = FileSystem as unknown as {
  __fs: { readFile(uri: string): string | undefined };
};

const PINNED_A = 'file:///document/offline-audio/A.mp3';

function readyEntry(version?: string) {
  return { A: { trackId: 'A', status: 'ready' as const, uri: PINNED_A, ...(version === undefined ? {} : { version }) } };
}

async function flush(rounds = 20): Promise<void> {
  for (let i = 0; i < rounds; i += 1) {
    await Promise.resolve();
  }
}

beforeEach(() => {
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  fetchAudioUrlsMock.mockReset();
  fetchAudioUrlsMock.mockResolvedValue([]);
});

describe('pinnedUri — version gate', () => {
  it('returns the local copy when the pinned version is the one the server currently serves', () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });

    expect(pinnedUri('A', 'v1')).toBe(PINNED_A);
  });

  it('REGRESSION: refuses the local copy when the server has since re-acquired the track under a new version', () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });

    expect(pinnedUri('A', 'v2')).toBeUndefined();
  });

  it('REGRESSION: refuses a local copy pinned before versions existed once the server reports a version', () => {
    usePinnedStore.setState({ entries: readyEntry(undefined) });

    expect(pinnedUri('A', 'v2')).toBeUndefined();
  });

  it('serves the local copy when the caller has no version to check against — an offline load must still play', () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });

    expect(pinnedUri('A', undefined)).toBe(PINNED_A);
  });

  it('serves the local copy when the server itself reports no version, so a never-re-acquired track is not re-downloaded on every play', () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });

    expect(pinnedUri('A', '')).toBe(PINNED_A);
  });

  it('stays gated on status: a version match does not resurrect a failed entry', () => {
    usePinnedStore.setState({
      entries: { A: { trackId: 'A', status: 'failed', uri: PINNED_A, version: 'v1' } },
    });

    expect(pinnedUri('A', 'v1')).toBeUndefined();
  });
});

describe('pinnedUri — self-healing on a version mismatch', () => {
  it('REGRESSION: a stale pinned file is replaced with the current audio without any acquisition event arriving', async () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });
    fetchAudioUrlsMock.mockResolvedValue([
      { trackId: 'A', url: 'https://cdn.example/A.mp3?gen=2', version: 'v2' },
    ]);

    expect(pinnedUri('A', 'v2')).toBeUndefined();

    await act(async () => {
      await flush();
    });

    expect(usePinnedStore.getState().entries['A']).toEqual({
      trackId: 'A',
      status: 'ready',
      uri: PINNED_A,
      version: 'v2',
    });
    expect(__fs.readFile(PINNED_A)).toBe('downloaded:https://cdn.example/A.mp3?gen=2');
    expect(pinnedUri('A', 'v2')).toBe(PINNED_A);
  });

  it('does not re-pin a track that was never pinned', () => {
    pinnedUri('never-pinned', 'v2');

    expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
    expect(usePinnedStore.getState().entries['never-pinned']).toBeUndefined();
  });

  it('does not stack re-pins while the replacement is already in flight', () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });
    const pending = new Promise<ResolvedAudioUrl[]>(() => {});
    fetchAudioUrlsMock.mockReturnValue(pending);

    pinnedUri('A', 'v2');
    pinnedUri('A', 'v2');
    pinnedUri('A', 'v2');

    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);
    expect(usePinnedStore.getState().queue).toEqual([]);
  });
});

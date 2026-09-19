import { act } from '@testing-library/react-native';
import * as FileSystem from 'expo-file-system';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';

import { resolvePinnedUri, usePinnedStore, type PinnedEntry } from '../pinnedStore';
import { asTrackId } from '@shared/api-client/ids';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const fetchAudioUrlsMock = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

const { __fs } = FileSystem as unknown as {
  __fs: { readFile(uri: string): string | undefined };
};

const PINNED_A = 'file:///document/offline-audio/A.mp3';

function readyEntry(version?: string) {
  return {
    A: {
      trackId: asTrackId('A'),
      status: 'ready' as const,
      uri: PINNED_A,
      ...(version === undefined ? {} : { version }),
    },
  };
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

describe('resolvePinnedUri — version gate', () => {
  it('returns the local copy when the pinned version is the one the server currently serves', () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });

    expect(resolvePinnedUri(asTrackId('A'), 'v1')).toBe(PINNED_A);
  });

  it('REGRESSION: refuses the local copy when the server has since re-acquired the track under a new version', () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });

    expect(resolvePinnedUri(asTrackId('A'), 'v2')).toBeUndefined();
  });

  it('REGRESSION: refuses a local copy pinned before versions existed once the server reports a version', () => {
    usePinnedStore.setState({ entries: readyEntry(undefined) });

    expect(resolvePinnedUri(asTrackId('A'), 'v2')).toBeUndefined();
  });

  it('serves the local copy when the caller has no version to check against — an offline load must still play', () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });

    expect(resolvePinnedUri(asTrackId('A'), undefined)).toBe(PINNED_A);
  });

  it('serves the local copy when the server itself reports no version, so a never-re-acquired track is not re-downloaded on every play', () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });

    expect(resolvePinnedUri(asTrackId('A'), '')).toBe(PINNED_A);
  });

  it('stays gated on status: a version match does not resurrect a failed entry', () => {
    usePinnedStore.setState({
      // #1766 made a failed entry carrying a uri and a version unrepresentable, so the fixture is
      // forced past the type: the status check stays as defence in depth for state planted that way.
      entries: {
        A: { trackId: asTrackId('A'), status: 'failed', uri: PINNED_A, version: 'v1' },
      } as unknown as Record<string, PinnedEntry>,
    });

    expect(resolvePinnedUri(asTrackId('A'), 'v1')).toBeUndefined();
  });
});

describe('resolvePinnedUri — self-healing on a version mismatch', () => {
  it('refuses the stale copy and takes it off ready in the same call, so no ordering can serve it', () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });
    fetchAudioUrlsMock.mockReturnValue(new Promise<ResolvedAudioUrl[]>(() => {}));

    expect(resolvePinnedUri(asTrackId('A'), 'v2')).toBeUndefined();

    expect(usePinnedStore.getState().entries['A']?.status).not.toBe('ready');
    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);
  });

  it('REGRESSION: a stale pinned file is replaced with the current audio without any acquisition event arriving', async () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });
    fetchAudioUrlsMock.mockResolvedValue([
      { trackId: 'A', url: 'https://cdn.example/A.mp3?gen=2', version: 'v2' },
    ]);

    resolvePinnedUri(asTrackId('A'), 'v2');

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
    expect(resolvePinnedUri(asTrackId('A'), 'v2')).toBe(PINNED_A);
  });

  it('leaves a matching version in place — no re-pin when the local copy is current', () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });

    expect(resolvePinnedUri(asTrackId('A'), 'v1')).toBe(PINNED_A);

    expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
    expect(usePinnedStore.getState().queue).toEqual([]);
  });

  it('does not re-pin a track that was never pinned', () => {
    expect(resolvePinnedUri(asTrackId('never-pinned'), 'v2')).toBeUndefined();

    expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
    expect(usePinnedStore.getState().entries['never-pinned']).toBeUndefined();
  });

  it('does not stack re-pins while the replacement is already in flight', () => {
    usePinnedStore.setState({ entries: readyEntry('v1') });
    const pending = new Promise<ResolvedAudioUrl[]>(() => {});
    fetchAudioUrlsMock.mockReturnValue(pending);

    resolvePinnedUri(asTrackId('A'), 'v2');
    resolvePinnedUri(asTrackId('A'), 'v2');
    resolvePinnedUri(asTrackId('A'), 'v2');

    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);
    expect(usePinnedStore.getState().queue).toEqual([]);
  });
});

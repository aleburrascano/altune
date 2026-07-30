import { act, render } from '@testing-library/react-native';
import * as FileSystem from 'expo-file-system';

import { OfflineReconcileBridge } from '../OfflineReconcileBridge';
import { usePinnedStore, type PinnedEntry } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn().mockResolvedValue([]) }));

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

const AUDIO_DIR = 'file:///document/offline-audio';

function audioUri(trackId: string): string {
  return `${AUDIO_DIR}/${trackId}.mp3`;
}

function resetStore(
  overrides: Partial<{
    entries: Record<string, PinnedEntry>;
    queue: string[];
    isWorking: boolean;
  }> = {},
): void {
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false, ...overrides });
}

beforeEach(() => {
  resetStore();
});

describe('OfflineReconcileBridge', () => {
  it('renders nothing', () => {
    const { toJSON } = render(<OfflineReconcileBridge />);

    expect(toJSON()).toBeNull();
  });

  it('is the only thing that turns reconcile on: a stale ready entry stays trusted until this mounts', () => {
    resetStore({
      entries: { stale: { trackId: 'stale', status: 'ready', uri: 'gone' } },
      isWorking: true,
    });
    expect(usePinnedStore.getState().entries['stale']).toEqual({
      trackId: 'stale',
      status: 'ready',
      uri: 'gone',
    });

    render(<OfflineReconcileBridge />);

    expect(usePinnedStore.getState().entries['stale']).toBeUndefined();
  });

  it('reconciles once on mount and does not pick up a later disk change without a remount', () => {
    resetStore({ entries: { flaky: { trackId: 'flaky', status: 'queued' } }, isWorking: true });

    render(<OfflineReconcileBridge />);
    expect(usePinnedStore.getState().entries['flaky']).toEqual({ trackId: 'flaky', status: 'queued' });

    __fs.seedFile(audioUri('flaky'), 'audio-bytes');
    act(() => {
      usePinnedStore.setState({ isWorking: false });
    });

    expect(usePinnedStore.getState().entries['flaky']).toEqual({ trackId: 'flaky', status: 'queued' });
  });

  it('reconciles exactly once per launch: a forced re-render of the mounted bridge does not call it again', () => {
    const realReconcile = usePinnedStore.getState().reconcile;
    const reconcileSpy = jest.fn(realReconcile);
    usePinnedStore.setState({ reconcile: reconcileSpy });

    const { rerender } = render(<OfflineReconcileBridge />);
    rerender(<OfflineReconcileBridge />);
    rerender(<OfflineReconcileBridge />);

    expect(reconcileSpy).toHaveBeenCalledTimes(1);

    usePinnedStore.setState({ reconcile: realReconcile });
  });

  it('remounting reconciles again without corrupting a still-healthy entry', () => {
    __fs.seedFile(audioUri('healthy'), 'audio-bytes');
    resetStore({
      entries: { healthy: { trackId: 'healthy', status: 'ready', uri: 'stale-but-real' } },
      isWorking: true,
    });

    const { unmount } = render(<OfflineReconcileBridge />);
    const afterFirst = usePinnedStore.getState().entries['healthy'];
    unmount();

    render(<OfflineReconcileBridge />);
    const afterSecond = usePinnedStore.getState().entries['healthy'];

    expect(afterSecond).toEqual(afterFirst);
    expect(afterSecond).toEqual({ trackId: 'healthy', status: 'ready', uri: audioUri('healthy') });
  });
});

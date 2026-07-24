jest.mock('../pinnedFiles', () => ({
  findPinned: jest.fn(() => null),
  deletePinned: jest.fn(),
  deleteAllPinned: jest.fn(),
  downloadPinned: jest.fn(async () => 'file:///offline/t1.mp3'),
  pinnedBytes: jest.fn(() => 0),
  formatBytes: jest.fn(() => '0 B'),
  pinnedDir: jest.fn(),
  extFromUrl: jest.fn(() => '.mp3'),
}));

jest.mock('@shared/api-client/audio', () => ({
  fetchAudioUrls: jest.fn(async (ids: string[]) =>
    ids.map((id) => ({ trackId: id, url: `https://cdn/${id}.mp3` })),
  ),
}));

jest.mock('expo-file-system', () => ({
  Paths: { document: '/doc' },
  Directory: class {
    exists = true;
    create() {}
  },
  File: class {
    exists = false;
    textSync() {
      return '{}';
    }
    write() {}
    delete() {}
  },
}));

import { fetchAudioUrls } from '@shared/api-client/audio';
import { findPinned, deletePinned, downloadPinned } from '../pinnedFiles';
import { pinnedUri, usePinnedStore } from '../pinnedStore';

const flush = async (): Promise<void> => {
  await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
  await new Promise((r) => setTimeout(r, 0));
};

beforeEach(() => {
  jest.clearAllMocks();
  (findPinned as jest.Mock).mockReturnValue(null);
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
});

it('takes a track from queued to ready', async () => {
  usePinnedStore.getState().pin('t1');
  expect(usePinnedStore.getState().entries.t1?.status).not.toBeUndefined();

  await flush();

  expect(usePinnedStore.getState().entries.t1).toEqual({
    trackId: 't1',
    status: 'ready',
    uri: 'file:///offline/t1.mp3',
  });
});

it('marks a track failed when no signed url comes back, without losing the entry', async () => {
  (fetchAudioUrls as jest.Mock).mockResolvedValueOnce([]);

  usePinnedStore.getState().pin('t1');
  await flush();

  expect(usePinnedStore.getState().entries.t1?.status).toBe('failed');
});

it('downloads one at a time', async () => {
  let concurrent = 0;
  let peak = 0;
  (downloadPinned as jest.Mock).mockImplementation(async () => {
    concurrent += 1;
    peak = Math.max(peak, concurrent);
    await new Promise((r) => setTimeout(r, 0));
    concurrent -= 1;
    return 'file:///offline/x.mp3';
  });

  usePinnedStore.getState().pinMany(['a', 'b', 'c']);
  await flush();
  await flush();

  expect(peak).toBe(1);
});

it('unpinning deletes the file and forgets the entry', () => {
  usePinnedStore.setState({
    entries: { t1: { trackId: 't1', status: 'ready', uri: 'file:///x' } },
  });

  usePinnedStore.getState().unpin('t1');

  expect(deletePinned).toHaveBeenCalledWith('t1');
  expect(usePinnedStore.getState().entries.t1).toBeUndefined();
});

it('deletes a file whose entry was dropped mid-download instead of stranding it', async () => {
  let releaseDownload: (uri: string) => void = () => {};
  (downloadPinned as jest.Mock).mockImplementationOnce(
    () =>
      new Promise<string>((resolve) => {
        releaseDownload = resolve;
      }),
  );

  usePinnedStore.getState().pin('t1');
  await flush();

  usePinnedStore.getState().unpinAll();
  releaseDownload('file:///offline/t1.mp3');
  await flush();

  expect(deletePinned).toHaveBeenCalledWith('t1');
  expect(usePinnedStore.getState().entries.t1).toBeUndefined();
});

it('does not re-queue a track that is already downloaded', () => {
  usePinnedStore.setState({
    entries: { t1: { trackId: 't1', status: 'ready', uri: 'file:///x' } },
  });

  usePinnedStore.getState().pin('t1');

  expect(usePinnedStore.getState().queue).toEqual([]);
});

describe('reconcile', () => {
  it('drops entries whose file has vanished', () => {
    usePinnedStore.setState({
      entries: { t1: { trackId: 't1', status: 'ready', uri: 'file:///gone' } },
    });
    (findPinned as jest.Mock).mockReturnValue(null);

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries.t1).toBeUndefined();
  });

  it('promotes an entry to ready when the file is present', () => {
    usePinnedStore.setState({ entries: { t1: { trackId: 't1', status: 'failed' } } });
    (findPinned as jest.Mock).mockReturnValue({ uri: 'file:///offline/t1.mp3' });

    usePinnedStore.getState().reconcile();

    expect(usePinnedStore.getState().entries.t1).toEqual({
      trackId: 't1',
      status: 'ready',
      uri: 'file:///offline/t1.mp3',
    });
  });

  it('resumes a download a kill interrupted rather than forgetting it', async () => {
    usePinnedStore.setState({ entries: { t1: { trackId: 't1', status: 'downloading' } } });
    (findPinned as jest.Mock).mockReturnValue(null);

    usePinnedStore.getState().reconcile();
    expect(usePinnedStore.getState().entries.t1?.status).toBe('downloading');

    await flush();
    expect(usePinnedStore.getState().entries.t1?.status).toBe('ready');
  });
});

describe('pinnedUri', () => {
  it('returns the local file only when the track is ready', () => {
    usePinnedStore.setState({
      entries: { t1: { trackId: 't1', status: 'ready', uri: 'file:///x' } },
    });
    expect(pinnedUri('t1')).toBe('file:///x');

    usePinnedStore.setState({ entries: { t1: { trackId: 't1', status: 'downloading' } } });
    expect(pinnedUri('t1')).toBeUndefined();
    expect(pinnedUri('nope')).toBeUndefined();
  });
});

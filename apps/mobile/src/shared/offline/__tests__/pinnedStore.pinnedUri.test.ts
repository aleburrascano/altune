import { pinnedUri, usePinnedStore, type PinnedEntry } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({
  fetchAudioUrls: jest.fn().mockResolvedValue([]),
}));

beforeEach(() => {
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
});

describe('pinnedUri — every branch and boundary of the status check', () => {
  it.each<[string, PinnedEntry | undefined, string | undefined]>([
    [
      'ready with a uri — plays with no connectivity',
      { trackId: 't1', status: 'ready', uri: 'file:///document/offline-audio/t1.mp3' },
      'file:///document/offline-audio/t1.mp3',
    ],
    [
      'ready without a uri — degrades to undefined (falls back to network) instead of crashing',
      { trackId: 't1', status: 'ready' },
      undefined,
    ],
    ['queued', { trackId: 't1', status: 'queued' }, undefined],
    ['downloading', { trackId: 't1', status: 'downloading' }, undefined],
    ['failed', { trackId: 't1', status: 'failed' }, undefined],
    [
      'failed but carrying a stray uri left over from a prior ready state — status still gates it',
      { trackId: 't1', status: 'failed', uri: 'file:///document/offline-audio/t1.mp3' },
      undefined,
    ],
    ['no entry at all for the track', undefined, undefined],
  ])('%s', (_label, entry, expected) => {
    usePinnedStore.setState({ entries: entry ? { t1: entry } : {} });

    expect(pinnedUri('t1')).toBe(expected);
  });
});

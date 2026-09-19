import { resolvePinnedUri, usePinnedStore, type PinnedEntry } from '../pinnedStore';
import { asTrackId } from '@shared/api-client/ids';

jest.mock('@shared/api-client/audio', () => ({
  fetchAudioUrls: jest.fn().mockResolvedValue([]),
}));

beforeEach(() => {
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
});

describe('resolvePinnedUri — every branch and boundary of the status check', () => {
  it.each<[string, PinnedEntry | undefined, string | undefined]>([
    [
      'ready with a uri — plays with no connectivity',
      { trackId: asTrackId('t1'), status: 'ready', uri: 'file:///document/offline-audio/t1.mp3' },
      'file:///document/offline-audio/t1.mp3',
    ],
    [
      'ready without a uri — degrades to undefined (falls back to network) instead of crashing',
      { trackId: asTrackId('t1'), status: 'ready' },
      undefined,
    ],
    ['queued', { trackId: asTrackId('t1'), status: 'queued' }, undefined],
    ['downloading', { trackId: asTrackId('t1'), status: 'downloading' }, undefined],
    ['failed', { trackId: asTrackId('t1'), status: 'failed' }, undefined],
    [
      'failed but carrying a stray uri left over from a prior ready state — status still gates it',
      { trackId: asTrackId('t1'), status: 'failed', uri: 'file:///document/offline-audio/t1.mp3' },
      undefined,
    ],
    ['no entry at all for the track', undefined, undefined],
  ])('%s', (_label, entry, expected) => {
    usePinnedStore.setState({ entries: entry ? { t1: entry } : {} });

    expect(resolvePinnedUri(asTrackId('t1'))).toBe(expected);
  });
});

import { asTrackId } from '@shared/api-client/ids';

import {
  downloadingEntry,
  failedEntry,
  queuedEntry,
  readyEntry,
  type PinnedEntry,
} from '../pinnedIndex';

const TRACK_ID = asTrackId('t1');
const AUDIO_URI = 'file:///document/offline-audio/t1.mp3';

describe('a pinned entry carries only the fields its status has', () => {
  // Compile-time guards: tsc fails if uri goes back to being optional for every status, which is
  // what let a ready entry name no file and a track with no file name a stale one (#1766).
  it('refuses a ready entry with no uri, and a track with no file that carries one', () => {
    // @ts-expect-error a ready entry names the file it downloaded, so its uri is not optional
    const readyWithoutUri: PinnedEntry = { trackId: TRACK_ID, status: 'ready' };
    // @ts-expect-error a queued entry has not downloaded anything yet, so it names no file
    const queuedWithUri: PinnedEntry = { trackId: TRACK_ID, status: 'queued', uri: AUDIO_URI };
    // @ts-expect-error a failed entry's download produced no file either
    const failedWithUri: PinnedEntry = { trackId: TRACK_ID, status: 'failed', uri: AUDIO_URI };

    expect([readyWithoutUri.status, queuedWithUri.status, failedWithUri.status]).toEqual([
      'ready',
      'queued',
      'failed',
    ]);
  });

  it('records a downloaded version only when the download reported one', () => {
    expect(readyEntry(TRACK_ID, AUDIO_URI)).toStrictEqual({
      trackId: TRACK_ID,
      status: 'ready',
      uri: AUDIO_URI,
    });
    expect(readyEntry(TRACK_ID, AUDIO_URI, 'v3')).toStrictEqual({
      trackId: TRACK_ID,
      status: 'ready',
      uri: AUDIO_URI,
      version: 'v3',
    });
  });

  it('builds a queued, downloading or failed entry from its track id alone', () => {
    expect(queuedEntry(TRACK_ID)).toStrictEqual({ trackId: TRACK_ID, status: 'queued' });
    expect(downloadingEntry(TRACK_ID)).toStrictEqual({ trackId: TRACK_ID, status: 'downloading' });
    expect(failedEntry(TRACK_ID)).toStrictEqual({ trackId: TRACK_ID, status: 'failed' });
  });
});

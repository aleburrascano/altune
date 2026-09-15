// Regression for issue #30 (secondary "same songs repeat" symptom): the native
// queue can hold the whole library, but only MAX_PRESIGN (25) upcoming tracks get
// a fresh signed URL at load time. Left alone, a long shuffle session eventually
// reaches unsigned tracks. refreshUpcomingPresign slides that window forward as
// the queue advances so tracks beyond the initial 25 get presigned before playing.

import { asTrackId } from '@shared/api-client/ids';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { loadNativeQueue, refreshUpcomingPresign } from '../loadNativeTrack';

import { libraryTrack } from './fixtures';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest
        .fn()
        .mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null }),
    },
  },
}));

function makeLibrary(count: number): PlaybackTrack[] {
  return Array.from({ length: count }, (_, i) =>
    libraryTrack({
      source: { kind: 'library', trackId: asTrackId(`t${i}`) },
      title: `Track t${i}`,
    }),
  );
}

// Every track id that has been sent to POST /v1/audio-urls so far (i.e. presigned).
function presignedTrackIds(): Set<string> {
  const ids = new Set<string>();
  for (const req of __http.requests as { path: string; body?: string }[]) {
    if (req.path !== '/v1/audio-urls' || typeof req.body !== 'string') continue;
    const parsed = JSON.parse(req.body) as { track_ids?: string[] };
    for (const id of parsed.track_ids ?? []) ids.add(id);
  }
  return ids;
}

beforeEach(() => {
  useQueueStore.getState().clearQueue();
  __http.reply('POST /v1/audio-urls', { json: { urls: [] } });
});

describe('refreshUpcomingPresign — presign window slides beyond the first 25 as the queue advances', () => {
  it('presigns only the first 25 tracks at queue start', async () => {
    const library = makeLibrary(60);
    useQueueStore.getState().loadQueue(library, 0, null);

    await loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), 0, { autoplay: false });

    const presigned = presignedTrackIds();
    expect(presigned.has('t0')).toBe(true);
    expect(presigned.has('t24')).toBe(true);
    // A track well past the initial window is NOT presigned yet — this is the cap
    // that caused the repeats in the car.
    expect(presigned.has('t30')).toBe(false);
  });

  it('presigns a track beyond the first 25 once the queue advances near the window edge', async () => {
    const library = makeLibrary(60);
    useQueueStore.getState().loadQueue(library, 0, null);
    await loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), 0, { autoplay: false });
    expect(presignedTrackIds().has('t30')).toBe(false);

    // The player has advanced to position 20 — within the refresh margin of the
    // initial window edge (position 24).
    useQueueStore.getState().skipToIndex(20);
    await refreshUpcomingPresign(20);

    // The window has slid forward: track 30, previously unsigned, is now presigned.
    expect(presignedTrackIds().has('t30')).toBe(true);
  });

  it('does not re-presign while the active track is still deep inside the presigned window', async () => {
    const library = makeLibrary(60);
    useQueueStore.getState().loadQueue(library, 0, null);
    await loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), 0, { autoplay: false });
    const requestsAfterLoad = __http.countFor('POST /v1/audio-urls');

    // Position 5 is far from the window edge (24), so no fresh presign is needed.
    useQueueStore.getState().skipToIndex(5);
    await refreshUpcomingPresign(5);

    expect(__http.countFor('POST /v1/audio-urls')).toBe(requestsAfterLoad);
  });
});

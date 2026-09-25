// Regression for issue #30 (secondary "same songs repeat" symptom): the native
// queue can hold the whole library, but only MAX_PRESIGN (25) upcoming tracks get
// a fresh signed URL at load time. Left alone, a long shuffle session eventually
// reaches unsigned tracks. refreshUpcomingPresign slides that window forward as
// the queue advances so tracks beyond the initial 25 get presigned before playing.

import { asTrackId } from '@shared/api-client/ids';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { Event } from 'react-native-track-player';

import { appendNativeTrack, loadNativeQueue, refreshUpcomingPresign } from '../loadNativeTrack';
import { usePlaybackErrorStore } from '../playbackErrorStore';
import { NATIVE_QUEUE_WINDOW } from '../presignWindow';
import { playbackService } from '../service';

import { libraryTrack } from './fixtures';

const { __http } = require('../../../../jest/doubles/fetch.js');
const { __player } = jest.requireMock('react-native-track-player');

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

// The track ids each TrackPlayer.add call marshalled across the bridge, in call order.
function addedTrackKeys(): string[][] {
  return (__player.calls('add') as unknown[][]).map(([arg]) =>
    (Array.isArray(arg) ? arg : [arg]).map((t) => (t as { id: string }).id),
  );
}

function loadedQueue(count: number, startIndex: number): Promise<void> {
  useQueueStore.getState().loadQueue(makeLibrary(count), startIndex, null);
  return loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), startIndex, {
    autoplay: false,
  });
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

// Regression for #1732: MAX_PRESIGN bounded only how many tracks got a signed URL, never
// how many track objects crossed the bridge. A saved-queue restore can hold
// REHYDRATE_LIMIT (2000) tracks, and every one of them was marshalled into a single
// TrackPlayer.add at load and again on every presign refresh.
const RESTORED_QUEUE_LENGTH = 2000;

function appendedTrack(): PlaybackTrack {
  return libraryTrack({
    source: { kind: 'library', trackId: asTrackId('t-appended') },
    title: 'Appended',
  });
}

describe('the native queue window — a queue far larger than the presign window', () => {
  it('marshals one window at load, not the whole queue', async () => {
    await loadedQueue(RESTORED_QUEUE_LENGTH, 0);

    const [added] = addedTrackKeys();
    expect(added).toHaveLength(NATIVE_QUEUE_WINDOW + 1);
    expect(added?.at(-1)).toBe(`library:t${NATIVE_QUEUE_WINDOW}`);
  });

  it('keeps every position before the active track, so a native index is still a queue index', async () => {
    await loadedQueue(RESTORED_QUEUE_LENGTH, 300);

    const [added] = addedTrackKeys();
    expect(added?.[300]).toBe('library:t300');
    expect(added).toHaveLength(301 + NATIVE_QUEUE_WINDOW);
  });

  it('slides the window forward on a presign refresh instead of re-pushing the remaining queue', async () => {
    await loadedQueue(RESTORED_QUEUE_LENGTH, 0);
    const addsAtLoad = addedTrackKeys().length;

    useQueueStore.getState().skipToIndex(20);
    await refreshUpcomingPresign(20);

    const slid = addedTrackKeys()[addsAtLoad];
    expect(slid).toHaveLength(NATIVE_QUEUE_WINDOW);
    expect(slid?.[0]).toBe('library:t21');
    expect(slid?.at(-1)).toBe(`library:t${20 + NATIVE_QUEUE_WINDOW}`);
  });

  it('leaves an append beyond the window edge to the next slide, so it cannot play early', async () => {
    await loadedQueue(RESTORED_QUEUE_LENGTH, 0);
    const addsAtLoad = addedTrackKeys().length;

    useQueueStore.getState().enqueue(appendedTrack());
    await appendNativeTrack(appendedTrack());

    expect(addedTrackKeys()).toHaveLength(addsAtLoad);
  });

  it('appends to the native queue while the whole queue still fits inside the window', async () => {
    await loadedQueue(3, 0);

    useQueueStore.getState().enqueue(appendedTrack());
    await appendNativeTrack(appendedTrack());

    expect(addedTrackKeys().at(-1)).toEqual(['library:t-appended']);
  });
});

// Regression for #1725: the window was marked presigned *before* the native reorder that
// installs the signed URLs. A rejected reorder (queue-lock timeout, bridge error) left
// `presignedThrough` covering a block whose URLs never arrived, so no later slide fired
// and the failure was neither classified nor reported — the service `void`-ed the call.
const NATIVE_QUEUE_TIMEOUT_MESSAGE = 'Playback command timed out after 15s';

function failNextReorder(): void {
  __player.failNext('add', new Error(NATIVE_QUEUE_TIMEOUT_MESSAGE));
}

function settled(): Promise<void> {
  return new Promise((resolve) => setImmediate(resolve));
}

describe('a presign slide whose native reorder rejects', () => {
  it('leaves the window unmarked, so the next active-track change slides it again', async () => {
    await loadedQueue(60, 0);
    useQueueStore.getState().skipToIndex(20);
    failNextReorder();
    await expect(refreshUpcomingPresign(20)).rejects.toThrow(NATIVE_QUEUE_TIMEOUT_MESSAGE);

    const presignsAfterFailure = __http.countFor('POST /v1/audio-urls');
    await refreshUpcomingPresign(20);

    expect(__http.countFor('POST /v1/audio-urls')).toBe(presignsAfterFailure + 1);
    expect(addedTrackKeys().at(-1)?.[0]).toBe('library:t21');
  });

  it('marks the window only once a slide has installed the URLs', async () => {
    await loadedQueue(60, 0);
    useQueueStore.getState().skipToIndex(20);
    failNextReorder();
    await expect(refreshUpcomingPresign(20)).rejects.toThrow(NATIVE_QUEUE_TIMEOUT_MESSAGE);
    await refreshUpcomingPresign(20);

    const presignsAfterRetry = __http.countFor('POST /v1/audio-urls');
    await refreshUpcomingPresign(20);

    expect(__http.countFor('POST /v1/audio-urls')).toBe(presignsAfterRetry);
  });
});

describe('the playback service reacting to a failed presign slide', () => {
  function onActiveTrackChanged(): (data: { index: number; track: { id: string } }) => void {
    const registration = __player
      .calls('addEventListener')
      .find(([event]: [unknown]) => event === Event.PlaybackActiveTrackChanged);
    if (!registration) throw new Error('no PlaybackActiveTrackChanged listener was registered');
    return registration[1];
  }

  afterEach(() => {
    usePlaybackErrorStore.getState().clear();
  });

  it('reports the failure against the playing track instead of dropping it', async () => {
    await loadedQueue(60, 0);
    await playbackService();
    failNextReorder();

    onActiveTrackChanged()({ index: 20, track: { id: 'library:t20' } });
    await settled();

    expect(usePlaybackErrorStore.getState()).toMatchObject({
      key: 'library:t20',
      kind: 'queue_update_failed',
    });
  });
});

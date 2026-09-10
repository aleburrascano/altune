import type { PlaybackTrack, QueueSource } from '@shared/playback/types';

import {
  buildTrackPayload,
  hasCrossedListenThreshold,
  LISTEN_THRESHOLD_MS,
  listenThresholdMs,
  trackKey,
} from '../signals';

const libraryTrack: PlaybackTrack = {
  source: { kind: 'library', trackId: 'trk-1' },
  title: 'A Title',
  artist: 'An Artist',
  artworkUrl: null,
};

const previewTrack: PlaybackTrack = {
  source: { kind: 'preview', previewUrl: 'https://cdn.example/p.mp3' },
  title: 'A Title',
  artist: 'An Artist',
  artworkUrl: null,
};

describe('listenThresholdMs — the dwell that counts as a listen', () => {
  it('is the flat threshold when the duration is unknown (zero)', () => {
    expect(listenThresholdMs(0)).toBe(LISTEN_THRESHOLD_MS);
  });

  it('is the flat threshold for a negative duration', () => {
    expect(listenThresholdMs(-1)).toBe(LISTEN_THRESHOLD_MS);
  });

  it('is half the duration for a short track below twice the threshold', () => {
    expect(listenThresholdMs(40_000)).toBe(20_000);
  });

  it('caps at the flat threshold exactly where half-duration meets it', () => {
    expect(listenThresholdMs(60_000)).toBe(LISTEN_THRESHOLD_MS);
  });

  it('stays capped at the flat threshold for a long track', () => {
    expect(listenThresholdMs(600_000)).toBe(LISTEN_THRESHOLD_MS);
  });
});

describe('hasCrossedListenThreshold — has the listen dwell been reached', () => {
  it('is false just below the threshold', () => {
    expect(hasCrossedListenThreshold(19_999, 40_000)).toBe(false);
  });

  it('is true exactly at the threshold', () => {
    expect(hasCrossedListenThreshold(20_000, 40_000)).toBe(true);
  });
});

describe('trackKey — the telemetry identity of a track', () => {
  it('keys a library track by its track id and title', () => {
    expect(trackKey(libraryTrack)).toBe('lib:trk-1|A Title');
  });

  it('keys a preview track by its preview url and title', () => {
    expect(trackKey(previewTrack)).toBe('prev:https://cdn.example/p.mp3|A Title');
  });
});

describe('buildTrackPayload — the telemetry payload for a track event', () => {
  const playlistSource: QueueSource = { kind: 'playlist', playlistId: 'pl-1', name: 'Mix' };

  it('carries the library track id and the queue surface', () => {
    const payload = buildTrackPayload(libraryTrack, playlistSource);

    expect(payload).toEqual({
      title: 'A Title',
      artist: 'An Artist',
      source_kind: 'library',
      track_id: 'trk-1',
      surface: 'playlist',
    });
  });

  it('reports a null track id for a preview and a null surface with no queue', () => {
    const payload = buildTrackPayload(previewTrack, null);

    expect(payload.track_id).toBeNull();
    expect(payload.source_kind).toBe('preview');
    expect(payload.surface).toBeNull();
  });

  it('omits result_signature when the track carries none', () => {
    const payload = buildTrackPayload(libraryTrack, null);

    expect('result_signature' in payload).toBe(false);
  });

  it('includes result_signature when the track carries one', () => {
    const payload = buildTrackPayload({ ...libraryTrack, resultSignature: 'sig-9' }, null);

    expect(payload.result_signature).toBe('sig-9');
  });

  it('omits dwell_ms when no dwell is supplied', () => {
    const payload = buildTrackPayload(libraryTrack, null);

    expect('dwell_ms' in payload).toBe(false);
  });

  it('rounds a supplied dwell to whole milliseconds', () => {
    const payload = buildTrackPayload(libraryTrack, null, 1234.6);

    expect(payload.dwell_ms).toBe(1235);
  });

  it('includes a zero dwell rather than dropping it', () => {
    const payload = buildTrackPayload(libraryTrack, null, 0);

    expect(payload.dwell_ms).toBe(0);
  });
});

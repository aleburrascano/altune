import { asPlaylistId } from '@shared/apiClient/ids';
import type { QueueSource } from '@shared/playback/queueOrder';

import { buildTrackPayload, hasCrossedListenThreshold } from '@features/playback/domain/signals';

import {
  libraryTrack as buildLibraryTrack,
  previewTrack as buildPreviewTrack,
} from '@features/playback/__tests__/fixtures';

const LISTEN_THRESHOLD_MS = 30_000;
const SHORT_TRACK_MS = 40_000;
const HALF_SHORT_TRACK_MS = 20_000;
const CAP_MEETS_HALF_MS = 60_000;
const LONG_TRACK_MS = 600_000;

function expectListenThreshold(durationMs: number, thresholdMs: number): void {
  expect(hasCrossedListenThreshold(thresholdMs - 1, durationMs)).toBe(false);
  expect(hasCrossedListenThreshold(thresholdMs, durationMs)).toBe(true);
}

const libraryTrack = buildLibraryTrack();
const previewTrack = buildPreviewTrack();

describe('listenThresholdMs — the dwell that counts as a listen', () => {
  it('is the flat threshold when the duration is unknown (zero)', () => {
    expectListenThreshold(0, LISTEN_THRESHOLD_MS);
  });

  it('is the flat threshold for a negative duration', () => {
    expectListenThreshold(-1, LISTEN_THRESHOLD_MS);
  });

  it('is half the duration for a short track below twice the threshold', () => {
    expectListenThreshold(SHORT_TRACK_MS, HALF_SHORT_TRACK_MS);
  });

  it('caps at the flat threshold exactly where half-duration meets it', () => {
    expectListenThreshold(CAP_MEETS_HALF_MS, LISTEN_THRESHOLD_MS);
  });

  it('stays capped at the flat threshold for a long track', () => {
    expectListenThreshold(LONG_TRACK_MS, LISTEN_THRESHOLD_MS);
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

describe('buildTrackPayload — the telemetry payload for a track event', () => {
  const playlistSource: QueueSource = {
    kind: 'playlist',
    playlistId: asPlaylistId('pl-1'),
    name: 'Mix',
  };

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

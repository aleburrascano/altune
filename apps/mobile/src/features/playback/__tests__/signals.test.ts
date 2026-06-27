import {
  buildTrackPayload,
  hasCrossedListenThreshold,
  listenThresholdMs,
  trackKey,
} from '../signals';
import type { PlaybackTrack, QueueSource } from '@shared/playback/types';

function libTrack(over: Partial<PlaybackTrack> = {}): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: 't1' },
    title: 'Hello',
    artist: 'Adele',
    artworkUrl: null,
    durationSeconds: 300,
    resultSignature: 'track|hello|adele',
    searchId: 's1',
    ...over,
  };
}

describe('listenThresholdMs', () => {
  it('is 50% of duration when that is under 30s', () => {
    expect(listenThresholdMs(40000)).toBe(20000); // 50% of 40s = 20s
  });
  it('caps at 30s for long tracks', () => {
    expect(listenThresholdMs(300000)).toBe(30000); // 50% of 5min capped at 30s
  });
  it('falls back to 30s when duration is unknown', () => {
    expect(listenThresholdMs(0)).toBe(30000);
  });
});

describe('hasCrossedListenThreshold', () => {
  it('fires at 30s for a long track', () => {
    expect(hasCrossedListenThreshold(29999, 300000)).toBe(false);
    expect(hasCrossedListenThreshold(30000, 300000)).toBe(true);
  });
  it('fires at 50% for a short track', () => {
    expect(hasCrossedListenThreshold(19999, 40000)).toBe(false);
    expect(hasCrossedListenThreshold(20000, 40000)).toBe(true);
  });
});

describe('buildTrackPayload', () => {
  it('carries identity, surface, signature, and dwell', () => {
    const queueSource: QueueSource = { kind: 'search', query: 'adele' };
    const payload = buildTrackPayload(libTrack(), queueSource, 12345.7);
    expect(payload).toEqual({
      title: 'Hello',
      artist: 'Adele',
      source_kind: 'library',
      track_id: 't1',
      surface: 'search',
      result_signature: 'track|hello|adele',
      dwell_ms: 12346,
    });
  });

  it('omits dwell when not provided and nulls a preview track id', () => {
    const preview = libTrack({
      source: { kind: 'preview', previewUrl: 'https://p' },
      resultSignature: undefined,
    });
    const payload = buildTrackPayload(preview, null);
    expect(payload.track_id).toBeNull();
    expect(payload.surface).toBeNull();
    expect(payload.result_signature).toBeNull();
    expect(payload.dwell_ms).toBeUndefined();
  });
});

describe('trackKey', () => {
  it('distinguishes library and preview sources', () => {
    expect(trackKey(libTrack())).toBe('lib:t1|Hello');
    expect(
      trackKey(libTrack({ source: { kind: 'preview', previewUrl: 'u' } })),
    ).toBe('prev:u|Hello');
  });
});

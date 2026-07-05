import { derivePlaybackState, type DerivePlaybackStateInput } from '../derivePlaybackState';

import type { PlaybackTrack } from '@shared/playback/types';

const track: PlaybackTrack = {
  source: { kind: 'library', trackId: 't1' },
  title: 'Song',
  artist: 'Artist',
  artworkUrl: null,
  durationSeconds: 200,
};

function input(overrides: Partial<DerivePlaybackStateInput> = {}): DerivePlaybackStateInput {
  return {
    track,
    errorMessage: null,
    isBuffering: false,
    isEnded: false,
    isPlaying: false,
    positionMs: 60_000,
    durationMs: 200_000,
    ...overrides,
  };
}

describe('derivePlaybackState', () => {
  it('preserves position while buffering (resume flicker fix)', () => {
    // Priming the native queue on resume enters Buffering with the scrubber
    // already seeded to 60s. It must NOT snap to 0.
    const s = derivePlaybackState(input({ isBuffering: true }));
    expect(s.status).toBe('loading');
    expect(s.positionMs).toBe(60_000);
    expect(s.durationMs).toBe(200_000);
  });

  it('returns idle with no track', () => {
    expect(derivePlaybackState(input({ track: null })).status).toBe('idle');
  });

  it('reports error over everything else', () => {
    const s = derivePlaybackState(input({ errorMessage: 'boom', isBuffering: true }));
    expect(s.status).toBe('error');
    expect(s.errorMessage).toBe('boom');
  });

  it('reports ended at duration', () => {
    const s = derivePlaybackState(input({ isEnded: true }));
    expect(s.status).toBe('ended');
    expect(s.positionMs).toBe(200_000);
  });

  it('reports playing/paused with the live position', () => {
    expect(derivePlaybackState(input({ isPlaying: true })).status).toBe('playing');
    expect(derivePlaybackState(input({ isPlaying: false })).status).toBe('paused');
    expect(derivePlaybackState(input({ isPlaying: true })).positionMs).toBe(60_000);
  });
});

import type { PlaybackTrack } from '@shared/playback/types';

import { derivePlaybackState, type DerivePlaybackStateInput } from '../derivePlaybackState';
import type { RedactedPlaybackFailure } from '../redactPlaybackError';

const TRACK: PlaybackTrack = {
  source: { kind: 'preview', previewUrl: 'https://cdn.example/p.mp3' },
  title: 'A Title',
  artist: 'An Artist',
  artworkUrl: null,
};

const GONE: RedactedPlaybackFailure = { kind: 'not_found', message: 'This track is gone' };
const OFFLINE: RedactedPlaybackFailure = { kind: 'network', message: 'This track is gone' };

function input(overrides: Partial<DerivePlaybackStateInput> = {}): DerivePlaybackStateInput {
  return {
    track: TRACK,
    failure: null,
    phase: 'paused',
    positionMs: 1234,
    durationMs: 5000,
    ...overrides,
  };
}

describe('derivePlaybackState — status precedence', () => {
  it('is idle with a null track, ignoring every other signal', () => {
    const state = derivePlaybackState(input({ track: null, phase: 'playing', failure: OFFLINE }));

    expect(state.status).toBe('idle');
    expect(state.track).toBeNull();
    expect(state.positionMs).toBe(0);
    expect(state.durationMs).toBe(0);
    expect(state.errorMessage).toBeNull();
    expect(state.errorKind).toBeNull();
  });

  it('is error when a failure is present, whatever the phase', () => {
    for (const phase of ['loading', 'playing', 'paused', 'ended'] as const) {
      const state = derivePlaybackState(input({ failure: OFFLINE, phase }));

      expect(state.status).toBe('error');
      expect(state.track).toBe(TRACK);
      expect(state.errorMessage).toBe(OFFLINE.message);
      expect(state.positionMs).toBe(0);
      expect(state.durationMs).toBe(0);
    }
  });

  it('is loading in the loading phase', () => {
    const state = derivePlaybackState(input({ phase: 'loading' }));

    expect(state.status).toBe('loading');
    expect(state.positionMs).toBe(1234);
    expect(state.durationMs).toBe(5000);
    expect(state.errorMessage).toBeNull();
    expect(state.errorKind).toBeNull();
  });

  it('is ended in the ended phase, pinning position to duration', () => {
    const state = derivePlaybackState(input({ phase: 'ended', positionMs: 10 }));

    expect(state.status).toBe('ended');
    expect(state.positionMs).toBe(5000);
    expect(state.durationMs).toBe(5000);
  });

  it('is playing in the playing phase', () => {
    const state = derivePlaybackState(input({ phase: 'playing' }));

    expect(state.status).toBe('playing');
    expect(state.positionMs).toBe(1234);
    expect(state.durationMs).toBe(5000);
    expect(state.errorMessage).toBeNull();
  });

  it('is paused in the paused phase', () => {
    const state = derivePlaybackState(input({ phase: 'paused' }));

    expect(state.status).toBe('paused');
    expect(state.positionMs).toBe(1234);
    expect(state.durationMs).toBe(5000);
  });
});

describe('derivePlaybackState — the failure kind the UI branches on', () => {
  it('tells a permanently gone track apart from a transient network failure', () => {
    const gone = derivePlaybackState(input({ failure: GONE }));
    const offline = derivePlaybackState(input({ failure: OFFLINE }));

    expect(gone.status).toBe(offline.status);
    expect(gone.errorMessage).toBe(offline.errorMessage);
    expect(gone.errorKind).toBe('not_found');
    expect(offline.errorKind).toBe('network');
  });
});

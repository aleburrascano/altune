import type { PlaybackTrack } from '@shared/playback/types';

import { derivePlaybackState, type DerivePlaybackStateInput } from '../derivePlaybackState';

const TRACK: PlaybackTrack = {
  source: { kind: 'preview', previewUrl: 'https://cdn.example/p.mp3' },
  title: 'A Title',
  artist: 'An Artist',
  artworkUrl: null,
};

function input(overrides: Partial<DerivePlaybackStateInput> = {}): DerivePlaybackStateInput {
  return {
    track: TRACK,
    errorMessage: null,
    isBuffering: false,
    isEnded: false,
    isPlaying: false,
    positionMs: 1234,
    durationMs: 5000,
    ...overrides,
  };
}

describe('derivePlaybackState — status precedence', () => {
  it('is idle with a null track, ignoring every other signal', () => {
    const state = derivePlaybackState(
      input({ track: null, isPlaying: true, isBuffering: true, errorMessage: 'x' }),
    );

    expect(state.status).toBe('idle');
    expect(state.track).toBeNull();
    expect(state.positionMs).toBe(0);
    expect(state.durationMs).toBe(0);
    expect(state.errorMessage).toBeNull();
  });

  it('is error when a message is present, even while buffering, ended, and playing', () => {
    const state = derivePlaybackState(
      input({ errorMessage: 'boom', isBuffering: true, isEnded: true, isPlaying: true }),
    );

    expect(state.status).toBe('error');
    expect(state.track).toBe(TRACK);
    expect(state.errorMessage).toBe('boom');
    expect(state.positionMs).toBe(0);
    expect(state.durationMs).toBe(0);
  });

  it('is loading when buffering wins over ended and playing', () => {
    const state = derivePlaybackState(input({ isBuffering: true, isEnded: true, isPlaying: true }));

    expect(state.status).toBe('loading');
    expect(state.positionMs).toBe(1234);
    expect(state.durationMs).toBe(5000);
    expect(state.errorMessage).toBeNull();
  });

  it('is ended when ended wins over playing, pinning position to duration', () => {
    const state = derivePlaybackState(input({ isEnded: true, isPlaying: true, positionMs: 10 }));

    expect(state.status).toBe('ended');
    expect(state.positionMs).toBe(5000);
    expect(state.durationMs).toBe(5000);
  });

  it('is playing when nothing else applies and isPlaying is true', () => {
    const state = derivePlaybackState(input({ isPlaying: true }));

    expect(state.status).toBe('playing');
    expect(state.positionMs).toBe(1234);
    expect(state.durationMs).toBe(5000);
    expect(state.errorMessage).toBeNull();
  });

  it('is paused when nothing else applies and isPlaying is false', () => {
    const state = derivePlaybackState(input({ isPlaying: false }));

    expect(state.status).toBe('paused');
    expect(state.positionMs).toBe(1234);
    expect(state.durationMs).toBe(5000);
  });
});

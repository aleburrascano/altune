import type { PlaybackContextValue, PlaybackSource, PlaybackStatus } from '../types';
import { isCurrentlyPlaying } from '../isCurrentlyPlaying';

type Playback = Pick<PlaybackContextValue, 'status' | 'track'>;

function playbackWith(status: PlaybackStatus, source: PlaybackSource | null): Playback {
  return {
    status,
    track: source ? { source, title: 'Title', artist: 'Artist', artworkUrl: null } : null,
  };
}

const LIBRARY_A: PlaybackSource = { kind: 'library', trackId: 'track-a' };
const LIBRARY_B: PlaybackSource = { kind: 'library', trackId: 'track-b' };
const PREVIEW_A: PlaybackSource = { kind: 'preview', previewUrl: 'https://cdn.example.com/a.mp3' };
const PREVIEW_B: PlaybackSource = { kind: 'preview', previewUrl: 'https://cdn.example.com/b.mp3' };

const ALL_STATUSES: PlaybackStatus[] = ['idle', 'loading', 'playing', 'paused', 'ended', 'error'];

describe('isCurrentlyPlaying', () => {
  it.each(ALL_STATUSES.map((status) => [status, status === 'playing' || status === 'loading'] as const))(
    'status %s with a matching library source -> %s',
    (status, expectedActive) => {
      expect(isCurrentlyPlaying(playbackWith(status, LIBRARY_A), LIBRARY_A)).toBe(expectedActive);
    },
  );

  it.each(ALL_STATUSES.map((status) => [status, status === 'playing' || status === 'loading'] as const))(
    'status %s with a matching preview source -> %s',
    (status, expectedActive) => {
      expect(isCurrentlyPlaying(playbackWith(status, PREVIEW_A), PREVIEW_A)).toBe(expectedActive);
    },
  );

  it('is inactive the moment before loading and active from loading onward', () => {
    expect(isCurrentlyPlaying(playbackWith('idle', LIBRARY_A), LIBRARY_A)).toBe(false);
    expect(isCurrentlyPlaying(playbackWith('loading', LIBRARY_A), LIBRARY_A)).toBe(true);
  });

  it('does not match a different library trackId while playing', () => {
    expect(isCurrentlyPlaying(playbackWith('playing', LIBRARY_A), LIBRARY_B)).toBe(false);
  });

  it('does not match a different preview url while loading', () => {
    expect(isCurrentlyPlaying(playbackWith('loading', PREVIEW_A), PREVIEW_B)).toBe(false);
  });

  it('does not match a library source against a preview Track carrying the same string', () => {
    const playback = playbackWith('playing', { kind: 'preview', previewUrl: 'shared-string' });

    expect(isCurrentlyPlaying(playback, { kind: 'library', trackId: 'shared-string' })).toBe(false);
  });

  it('does not match a preview source against a library Track carrying the same string', () => {
    const playback = playbackWith('playing', { kind: 'library', trackId: 'shared-string' });

    expect(isCurrentlyPlaying(playback, { kind: 'preview', previewUrl: 'shared-string' })).toBe(false);
  });

  it('is not currently playing when there is no current track, even while active', () => {
    expect(isCurrentlyPlaying(playbackWith('playing', null), LIBRARY_A)).toBe(false);
    expect(isCurrentlyPlaying(playbackWith('loading', null), PREVIEW_A)).toBe(false);
  });
});

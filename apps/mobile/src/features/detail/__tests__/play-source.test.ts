import type { PlaybackContextValue, PlaybackSource, PlaybackStatus } from '@shared/playback/types';

import { trackExtras } from '../extras-accessors';
import type { OwnedTrack } from '../hooks/useOwnedTrack';
import { isResultPlaying, resolvePlaySource } from '../play-source';

type Playback = Pick<PlaybackContextValue, 'status' | 'track'>;

function playbackWith(status: PlaybackStatus, source: PlaybackSource | null): Playback {
  return {
    status,
    track: source ? { source, title: 'Title', artist: 'Artist', artworkUrl: null } : null,
  };
}

const PREVIEW_URL = 'https://cdn.example.com/preview.mp3';
const UNOWNED = trackExtras({ preview_url: PREVIEW_URL });
const PENDING: OwnedTrack = { trackId: 'track-a', acquisitionStatus: 'pending' };
const READY: OwnedTrack = { trackId: 'track-a', acquisitionStatus: 'ready' };

describe('resolvePlaySource', () => {
  it('starts the preview while the saved Track is still acquiring', () => {
    expect(resolvePlaySource(UNOWNED, PENDING)).toEqual({
      kind: 'preview',
      previewUrl: PREVIEW_URL,
    });
  });

  it('starts the library Track once acquisition is ready', () => {
    expect(resolvePlaySource(UNOWNED, READY)).toEqual({ kind: 'library', trackId: 'track-a' });
  });

  it('has nothing to start for an unowned Track with no preview', () => {
    expect(resolvePlaySource(trackExtras({}), null)).toBeNull();
  });
});

describe('isResultPlaying', () => {
  it('stays true when acquisition completes mid-preview', () => {
    const playback = playbackWith('playing', { kind: 'preview', previewUrl: PREVIEW_URL });

    expect(isResultPlaying(playback, UNOWNED, PENDING)).toBe(true);
    expect(isResultPlaying(playback, UNOWNED, READY)).toBe(true);
  });

  it('is true while the library Track plays', () => {
    const playback = playbackWith('playing', { kind: 'library', trackId: 'track-a' });

    expect(isResultPlaying(playback, UNOWNED, READY)).toBe(true);
  });

  it('is false once the preview is paused', () => {
    const playback = playbackWith('paused', { kind: 'preview', previewUrl: PREVIEW_URL });

    expect(isResultPlaying(playback, UNOWNED, READY)).toBe(false);
  });

  it('is false while a different Track plays', () => {
    const playback = playbackWith('playing', { kind: 'library', trackId: 'track-b' });

    expect(isResultPlaying(playback, UNOWNED, READY)).toBe(false);
  });

  it('is false for a result with no trackId and no preview', () => {
    const playback = playbackWith('playing', { kind: 'library', trackId: 'track-a' });

    expect(isResultPlaying(playback, trackExtras({}), null)).toBe(false);
  });
});

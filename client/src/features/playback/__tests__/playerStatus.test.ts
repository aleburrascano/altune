import type { PlaybackTrack } from '@shared/playback/playbackTrack';
import type { TrackId } from '@shared/apiClient/ids';
import {
  canRetryPlaybackError,
  endedLabel,
  libraryTrackId,
  statusFlagsOf,
} from '@features/playback/domain/playerStatus';

describe('playerStatus', () => {
  it('labels an ended preview and an ended full track differently', () => {
    expect(endedLabel(true)).toBe('Preview ended');
    expect(endedLabel(false)).toBe('Finished');
  });

  it('derives exactly one flag from the status', () => {
    expect(statusFlagsOf('playing')).toEqual({ isPlaying: true, isEnded: false, isError: false });
    expect(statusFlagsOf('ended')).toEqual({ isPlaying: false, isEnded: true, isError: false });
    expect(statusFlagsOf('error')).toEqual({ isPlaying: false, isEnded: false, isError: true });
  });

  it('returns the library track id from a library source', () => {
    const track = { source: { kind: 'library', trackId: 't1' as TrackId } } as PlaybackTrack;
    expect(libraryTrackId(track)).toBe('t1');
  });

  it('returns undefined for a preview source', () => {
    const track = { source: { kind: 'preview', previewUrl: 'x' } } as PlaybackTrack;
    expect(libraryTrackId(track)).toBeUndefined();
  });
});

describe('canRetryPlaybackError — which kinds a retry can still rescue', () => {
  it.each([
    ['network', true],
    ['auth', true],
    ['queue_out_of_sync', true],
    ['queue_update_failed', true],
    ['unknown', true],
    ['not_found', false],
    ['decode', false],
  ] as const)('%s is retryable: %s', (kind, canRetry) => {
    expect(canRetryPlaybackError(kind)).toBe(canRetry);
  });

  it('offers a retry when nothing classified the failure', () => {
    expect(canRetryPlaybackError(null)).toBe(true);
  });
});

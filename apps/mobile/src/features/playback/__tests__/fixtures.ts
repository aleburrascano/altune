import { asTrackId } from '@shared/api-client/ids';
import type { PlaybackTrack } from '@shared/playback/types';

// The one PlaybackTrack fixture pair for playback specs. Add new required fields
// here so every spec picks them up; tests vary fields via overrides.
export function libraryTrack(overrides: Partial<PlaybackTrack> = {}): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: asTrackId('trk-1') },
    title: 'A Title',
    artist: 'An Artist',
    artworkUrl: null,
    ...overrides,
  };
}

export function previewTrack(overrides: Partial<PlaybackTrack> = {}): PlaybackTrack {
  return {
    source: { kind: 'preview', previewUrl: 'https://cdn.example/p.mp3' },
    title: 'A Title',
    artist: 'An Artist',
    artworkUrl: null,
    ...overrides,
  };
}

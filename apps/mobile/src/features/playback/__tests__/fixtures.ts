import { asTrackId } from '@shared/api-client/ids';
import type { PlaybackTrack } from '@shared/playback/types';

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

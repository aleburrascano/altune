import type { PlaybackTrack } from '../types';
import { trackKey } from '../trackKey';

function track(source: PlaybackTrack['source']): PlaybackTrack {
  return { source, title: 'Title', artist: 'Artist', artworkUrl: null };
}

describe('trackKey', () => {
  it('keys a library Track by its trackId', () => {
    expect(trackKey(track({ kind: 'library', trackId: 'abc-123' }))).toBe('library:abc-123');
  });

  it('keys a preview Track by its previewUrl', () => {
    expect(
      trackKey(track({ kind: 'preview', previewUrl: 'https://cdn.example.com/p.mp3' })),
    ).toBe('preview:https://cdn.example.com/p.mp3');
  });

  it('never collides a library key with a preview key carrying the same identity string', () => {
    const libraryKey = trackKey(track({ kind: 'library', trackId: 'shared-id' }));
    const previewKey = trackKey(track({ kind: 'preview', previewUrl: 'shared-id' }));

    expect(libraryKey).not.toBe(previewKey);
  });
});

import { audioStreamUrl } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { toNativeTrack } from '../nativeTrack';

function libraryTrack(overrides: Partial<PlaybackTrack> = {}): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: asTrackId('trk-1') },
    title: 'A Title',
    artist: 'An Artist',
    artworkUrl: null,
    ...overrides,
  };
}

function previewTrack(overrides: Partial<PlaybackTrack> = {}): PlaybackTrack {
  return {
    source: { kind: 'preview', previewUrl: 'https://cdn.example/p.mp3' },
    title: 'A Title',
    artist: 'An Artist',
    artworkUrl: null,
    ...overrides,
  };
}

describe('toNativeTrack — identity and metadata', () => {
  it('carries the track key as the native id and the display metadata', () => {
    const track = libraryTrack({ title: 'Song One', artist: 'Band' });

    const native = toNativeTrack(track);

    expect(native.id).toBe(trackKey(track));
    expect(native.title).toBe('Song One');
    expect(native.artist).toBe('Band');
  });

  it('uses the track artwork when it has one', () => {
    const native = toNativeTrack(libraryTrack({ artworkUrl: 'https://cdn.example/art.jpg' }));

    expect(native.artwork).toBe('https://cdn.example/art.jpg');
  });

  it('substitutes the placeholder artwork when the track has none, never passing null through', () => {
    const withArt = toNativeTrack(libraryTrack({ artworkUrl: 'https://cdn.example/art.jpg' }));
    const withoutArt = toNativeTrack(libraryTrack({ artworkUrl: null }));

    expect(withoutArt.artwork).not.toBeNull();
    expect(withoutArt.artwork).not.toBe(withArt.artwork);
  });
});

describe('toNativeTrack — url resolution', () => {
  it('prefers an explicit stream url over everything else', () => {
    const native = toNativeTrack(libraryTrack(), { streamUrl: 'file:///local/a.mp3' });

    expect(native.url).toBe('file:///local/a.mp3');
  });

  it('drops the auth headers when serving from an explicit stream url', () => {
    const native = toNativeTrack(libraryTrack(), {
      streamUrl: 'file:///local/a.mp3',
      headers: { Authorization: 'Bearer secret' },
    });

    expect(native.headers).toBeUndefined();
  });

  it('serves a preview track straight from its preview url', () => {
    const native = toNativeTrack(previewTrack({ source: { kind: 'preview', previewUrl: 'https://cdn.example/x.mp3' } }));

    expect(native.url).toBe('https://cdn.example/x.mp3');
  });

  it('resolves a library track without a stream url through the audio endpoint', () => {
    const track = libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-42') } });

    const native = toNativeTrack(track);

    expect(native.url).toBe(audioStreamUrl('trk-42'));
  });

  it('attaches the supplied auth headers to a library stream', () => {
    const native = toNativeTrack(libraryTrack(), { headers: { Authorization: 'Bearer secret' } });

    expect(native.headers).toEqual({ Authorization: 'Bearer secret' });
  });

  it('defaults a library stream to empty headers when none are supplied', () => {
    const native = toNativeTrack(libraryTrack());

    expect(native.headers).toEqual({});
  });
});

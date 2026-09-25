import TrackPlayer from 'react-native-track-player';

import { audioStreamUrl } from '@shared/api-client/audio';
import { ContractError } from '@shared/errors';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
import { trackKey } from '@shared/playback/trackKey';

import { activeNativeTrackId, toNativeTrack } from '../nativeTrack';

import { libraryTrack, previewTrack } from './fixtures';

const player = TrackPlayer as unknown as Record<string, jest.Mock>;

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
    const native = toNativeTrack(
      previewTrack({ source: { kind: 'preview', previewUrl: 'https://cdn.example/x.mp3' } }),
    );

    expect(native.url).toBe('https://cdn.example/x.mp3');
  });

  it.each([
    'file:///data/data/app.altune/files/token.json',
    'content://com.android.contacts/contacts/1',
    'http://cdn.example/x.mp3',
    'javascript:alert(1)',
    'data:audio/mpeg;base64,SUQz',
    '//cdn.example/x.mp3',
    'https://cdn.example/a\nfile:///etc/passwd',
    'https://',
  ])('refuses to aim the native player at non-https preview url %p (#1721)', (previewUrl) => {
    const track = previewTrack({ source: { kind: 'preview', previewUrl } });

    expect(() => toNativeTrack(track)).toThrow(ContractError);
  });

  it('resolves a library track without a stream url through the audio endpoint', () => {
    const track = libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-42') } });

    const native = toNativeTrack(track);

    expect(native.url).toBe(audioStreamUrl(asTrackId('trk-42')));
  });

  it.each(['a/b', 'a?b=1', 'a#frag', '../x/y?z#w'])(
    'refuses trackId %p smuggled past the brand rather than building another route (#944)',
    (trackId) => {
      const track = libraryTrack({ source: { kind: 'library', trackId: trackId as TrackId } });

      expect(() => toNativeTrack(track)).toThrow(ContractError);
    },
  );

  it('attaches the supplied auth headers to a library stream', () => {
    const native = toNativeTrack(libraryTrack(), { headers: { Authorization: 'Bearer secret' } });

    expect(native.headers).toEqual({ Authorization: 'Bearer secret' });
  });

  it('defaults a library stream to empty headers when none are supplied', () => {
    const native = toNativeTrack(libraryTrack());

    expect(native.headers).toEqual({});
  });
});

describe('activeNativeTrackId — reading back the id toNativeTrack wrote', () => {
  afterEach(() => {
    player.getActiveTrack!.mockReset();
  });

  it('hands back the key of the entry the player is on', async () => {
    const track = libraryTrack();
    player.getActiveTrack!.mockResolvedValue(toNativeTrack(track));

    await expect(activeNativeTrackId()).resolves.toBe(trackKey(track));
  });

  it('names no track when the player has none active', async () => {
    player.getActiveTrack!.mockResolvedValue(undefined);

    await expect(activeNativeTrackId()).resolves.toBeUndefined();
  });

  it('names no track when the player was torn down under the call', async () => {
    player.getActiveTrack!.mockRejectedValue(new Error('player is not initialized'));

    await expect(activeNativeTrackId()).resolves.toBeUndefined();
  });

  it('names no track when the active entry carries no id of ours', async () => {
    player.getActiveTrack!.mockResolvedValue({ url: 'https://cdn.example/x.mp3' });

    await expect(activeNativeTrackId()).resolves.toBeUndefined();
  });
});

import { asTrackId } from '@shared/api-client/ids';
import type { ResolvedAudioUrl } from '@shared/api-client/audio';
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

export function libraryTrackWithId(id: string): PlaybackTrack {
  return libraryTrack({ source: { kind: 'library', trackId: asTrackId(id) } });
}

export function libraryTrackList(count: number): PlaybackTrack[] {
  return Array.from({ length: count }, (_, i) =>
    libraryTrack({
      source: { kind: 'library', trackId: asTrackId(`t${i}`) },
      title: `Track t${i}`,
    }),
  );
}

export function previewTrackList(count: number): PlaybackTrack[] {
  return Array.from({ length: count }, (_, i) =>
    previewTrack({
      source: { kind: 'preview', previewUrl: `https://cdn.example/${i}.mp3` },
      title: `Track ${i}`,
    }),
  );
}

export function resolvedAudio(trackId: string): ResolvedAudioUrl {
  return { trackId, url: `https://cdn.example/${trackId}.mp3`, version: 'v1' };
}

export function cdnAudioUrls(
  ids: readonly string[],
): Record<string, { url: string; version: string }> {
  return Object.fromEntries(
    ids.map((id) => {
      const { url, version } = resolvedAudio(id);
      return [id, { url, version }];
    }),
  );
}

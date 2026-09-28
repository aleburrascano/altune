import { Image } from 'react-native';
import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import type { PlaybackTrack } from '@shared/playback/types';
import { isPlayablePreviewUrl } from '@shared/playback/previewUrl';
import { trackKey, type TrackKey } from '@shared/playback/trackKey';

import { audioStreamUrl } from '@shared/api-client/audio';
import { ContractError } from '@shared/errors';

const artworkPlaceholder = (): string =>
  Image.resolveAssetSource(require('../../../assets/artwork-placeholder.png')).uri;

function playablePreviewUrl(previewUrl: string): string {
  if (!isPlayablePreviewUrl(previewUrl)) throw new ContractError('PreviewUrl', 'not an https url');
  return previewUrl;
}

export function toNativeTrack(
  track: PlaybackTrack,
  opts: { streamUrl?: string | undefined; headers?: Record<string, string> | undefined } = {},
): AddTrack {
  const base = {
    id: trackKey(track),
    title: track.title,
    artist: track.artist,
    artwork: track.artworkUrl ?? artworkPlaceholder(),
  };
  if (opts.streamUrl) return { ...base, url: opts.streamUrl };
  if (track.source.kind === 'preview') {
    return { ...base, url: playablePreviewUrl(track.source.previewUrl) };
  }
  return { ...base, url: audioStreamUrl(track.source.trackId), headers: opts.headers ?? {} };
}

export function activeNativeTrackId(): Promise<TrackKey | undefined> {
  return TrackPlayer.getActiveTrack().then(
    (active) => (typeof active?.id === 'string' ? (active.id as TrackKey) : undefined),
    () => undefined,
  );
}

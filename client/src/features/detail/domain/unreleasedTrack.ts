import type { DiscoveryResult, DiscoverySource } from '@shared/apiClient/discovery';
import type { UnreleasedAudioLink, UnreleasedTrack } from '@shared/apiClient/enrichment';

const UNRELEASED_ALBUM = 'UNRELEASED';

const YOUTUBE_VIDEO_ID = /[?&]v=([A-Za-z0-9_-]{11})/;
const PIXELDRAIN_FILE_ID = /^https:\/\/pixeldrain\.com\/u\/([A-Za-z0-9]{6,16})$/;

type LinkMapper = (url: string) => DiscoverySource | null;

function sourceFromId(provider: string, pattern: RegExp): LinkMapper {
  return (url) => {
    const id = pattern.exec(url)?.[1];
    return id === undefined ? null : { provider, external_id: id, url };
  };
}

const LINK_SOURCES: Record<UnreleasedAudioLink['provider'], LinkMapper> = {
  soundcloud: (url) => ({ provider: 'soundcloud', external_id: url, url }),
  youtube: sourceFromId('youtube', YOUTUBE_VIDEO_ID),
  pixeldrain: sourceFromId('pixeldrain', PIXELDRAIN_FILE_ID),
};

function linkSource(link: UnreleasedAudioLink): DiscoverySource | null {
  return LINK_SOURCES[link.provider](link.url);
}

function linkSources(track: UnreleasedTrack): DiscoverySource[] {
  const sources: DiscoverySource[] = [];
  for (const link of track.audio_links) {
    const source = linkSource(link);
    if (source !== null) sources.push(source);
  }
  return sources;
}

export function isAcquirable(track: UnreleasedTrack): boolean {
  return linkSources(track).length > 0;
}

function unreleasedExtras(track: UnreleasedTrack): DiscoveryResult['extras'] {
  return { ...track.extras, album: UNRELEASED_ALBUM };
}

function unreleasedShown(track: UnreleasedTrack, artist: DiscoveryResult) {
  const shown = { title: track.title, subtitle: artist.title, image_url: track.cover_url };
  return { kind: 'track' as const, confidence: 'medium' as const, ...shown };
}

export function unreleasedAsResult(
  track: UnreleasedTrack,
  artist: DiscoveryResult,
): DiscoveryResult {
  const linked = { sources: linkSources(track), extras: unreleasedExtras(track) };
  return { ...unreleasedShown(track, artist), ...linked };
}

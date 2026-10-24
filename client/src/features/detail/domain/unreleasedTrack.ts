import type { DiscoveryResult, DiscoverySource } from '@shared/apiClient/discovery';
import type { UnreleasedAudioLink, UnreleasedTrack } from '@shared/apiClient/enrichment';

const UNRELEASED_ALBUM = 'UNRELEASED';

const YOUTUBE_VIDEO_ID = /[?&]v=([A-Za-z0-9_-]{11})/;

function youtubeVideoId(url: string): string | null {
  return YOUTUBE_VIDEO_ID.exec(url)?.[1] ?? null;
}

function linkSource(link: UnreleasedAudioLink): DiscoverySource | null {
  if (link.provider === 'soundcloud') {
    return { provider: 'soundcloud', external_id: link.url, url: link.url };
  }
  const videoId = youtubeVideoId(link.url);
  return videoId === null ? null : { provider: 'youtube', external_id: videoId, url: link.url };
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

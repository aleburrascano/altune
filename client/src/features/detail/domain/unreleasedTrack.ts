import type { DiscoveryResult, DiscoverySource } from '@shared/apiClient/discovery';
import type { UnreleasedAudioLink, UnreleasedTrack } from '@shared/apiClient/enrichment';

const UNRELEASED_ALBUM = 'UNRELEASED';

function linkSource({ provider, external_id, url }: UnreleasedAudioLink): DiscoverySource {
  return { provider, external_id, url };
}

export function isAcquirable(track: UnreleasedTrack): boolean {
  return track.audio_links.length > 0;
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
  const linked = { sources: track.audio_links.map(linkSource), extras: unreleasedExtras(track) };
  return { ...unreleasedShown(track, artist), ...linked };
}

import type { DiscoveryResult, DiscoverySource } from '@shared/apiClient/discovery';
import type {
  UnreleasedAudioLink,
  UnreleasedTrack,
  UnreleasedVersion,
} from '@shared/apiClient/enrichment';

const UNRELEASED_ALBUM = 'UNRELEASED';
const MIN_GROUPED_VERSIONS = 2;

function linkSource({ provider, external_id, url }: UnreleasedAudioLink): DiscoverySource {
  return { provider, external_id, url };
}

export function isAcquirable(track: UnreleasedTrack): boolean {
  return track.audio_links.length > 0;
}

function unreleasedExtras(track: UnreleasedTrack): DiscoveryResult['extras'] {
  return { ...track.extras, album: UNRELEASED_ALBUM };
}

function linkIds(links: UnreleasedAudioLink[]): string {
  return links.map((link) => `${link.provider}:${link.external_id}`).join(' ');
}

function isBestVersion(version: UnreleasedVersion, track: UnreleasedTrack): boolean {
  return linkIds(version.audio_links) === linkIds(track.audio_links);
}

export function hasManyVersions(track: UnreleasedTrack): boolean {
  return track.versions.length >= MIN_GROUPED_VERSIONS;
}

function savedTitle(track: UnreleasedTrack): string {
  const best = track.versions.find((version) => isBestVersion(version, track));
  return best?.title ?? track.title;
}

function unreleasedShown(track: UnreleasedTrack, artist: DiscoveryResult) {
  const shown = { title: savedTitle(track), subtitle: artist.title, image_url: track.cover_url };
  return { kind: 'track' as const, confidence: 'medium' as const, ...shown };
}

export function unreleasedAsResult(
  track: UnreleasedTrack,
  artist: DiscoveryResult,
): DiscoveryResult {
  const linked = { sources: track.audio_links.map(linkSource), extras: unreleasedExtras(track) };
  return { ...unreleasedShown(track, artist), ...linked };
}

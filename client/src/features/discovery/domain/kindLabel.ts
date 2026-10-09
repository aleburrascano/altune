import type { DiscoveryKind } from '@shared/api-client/discovery';

const KIND_LABELS: Record<DiscoveryKind, readonly [string, string]> = {
  artist: ['Artist', 'Artists'],
  album: ['Album', 'Albums'],
  track: ['Track', 'Tracks'],
};

export function kindLabel(kind: DiscoveryKind, opts?: { plural?: boolean }): string {
  return KIND_LABELS[kind][opts?.plural ? 1 : 0];
}

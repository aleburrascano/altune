import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';
import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedSearchHit } from '@shared/apiClient/enrichment';

import { UnreleasedRow } from '@features/detail/ui/unreleased/ArtistUnreleased';

interface UnreleasedSearchHitsProps {
  hits: UnreleasedSearchHit[];
  limit?: number;
}

interface UnreleasedSearchHitProps {
  hit: UnreleasedSearchHit;
}

function hitArtist({ artist }: UnreleasedSearchHit): DiscoveryResult {
  const shown = { title: artist.name, subtitle: null, image_url: null };
  return { ...shown, kind: 'artist', confidence: 'high', sources: [], extras: {} };
}

function hitKey({ artist, key }: UnreleasedSearchHit): string {
  return `${artist.provider}:${artist.external_id}:${key}`;
}

function UnreleasedSearchHitCard({ hit }: UnreleasedSearchHitProps): ReactElement {
  return (
    <View testID={`discover-unreleased-hit-${hit.key}`} style={styles.hit}>
      <UnreleasedRow artist={hitArtist(hit)} track={hit} />
      <Text variant="label" tone="secondary" numberOfLines={1}>
        {hit.artist.name}
      </Text>
    </View>
  );
}

export function UnreleasedSearchHits({ hits, limit }: UnreleasedSearchHitsProps): ReactElement {
  return (
    <View style={styles.grid}>
      {hits.slice(0, limit).map((hit) => (
        <UnreleasedSearchHitCard key={hitKey(hit)} hit={hit} />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.md },
  hit: { gap: spacing.xs },
});

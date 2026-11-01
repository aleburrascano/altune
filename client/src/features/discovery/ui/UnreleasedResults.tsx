import type { ReactElement } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';
import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedSearchHit } from '@shared/apiClient/enrichment';

import { UnreleasedRow } from '@features/detail';
import { SectionLabel } from '@features/discovery/ui/SectionLabel';

export const UNRELEASED_SECTION_CAP = 5;

type HitsProps = { hits: UnreleasedSearchHit[] };

function hitArtist({ artist }: UnreleasedSearchHit): DiscoveryResult {
  return {
    kind: 'artist',
    title: artist.name,
    subtitle: null,
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };
}

function hitKey({ artist, key }: UnreleasedSearchHit): string {
  return `${artist.provider}:${artist.external_id}:${key}`;
}

function UnreleasedHit({ hit }: { hit: UnreleasedSearchHit }): ReactElement {
  return (
    <View testID={`discover-unreleased-hit-${hit.key}`} style={styles.hit}>
      <UnreleasedRow artist={hitArtist(hit)} track={hit} />
      <Text variant="label" tone="secondary" numberOfLines={1}>
        {hit.artist.name}
      </Text>
    </View>
  );
}

function HitGrid({ hits }: HitsProps): ReactElement {
  return (
    <View style={styles.grid}>
      {hits.map((hit) => (
        <UnreleasedHit key={hitKey(hit)} hit={hit} />
      ))}
    </View>
  );
}

export function UnreleasedSection({ hits }: HitsProps): ReactElement | null {
  if (hits.length === 0) return null;
  return (
    <View testID="discover-unreleased-section" style={styles.section}>
      <SectionLabel style={styles.label}>UNRELEASED</SectionLabel>
      <HitGrid hits={hits.slice(0, UNRELEASED_SECTION_CAP)} />
    </View>
  );
}

export function UnreleasedAllHits({ hits }: HitsProps): ReactElement {
  return (
    <ScrollView testID="discover-unreleased-all" contentContainerStyle={styles.all}>
      <HitGrid hits={hits} />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  section: { marginBottom: spacing.xl },
  label: { marginBottom: spacing.sm, marginTop: spacing.sm },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.md },
  hit: { gap: spacing.xs },
  all: { paddingTop: spacing.sm, paddingBottom: spacing.xl },
});

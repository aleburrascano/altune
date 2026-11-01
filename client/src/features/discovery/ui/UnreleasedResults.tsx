import type { ReactElement } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';

import { spacing } from '@shared/ui';
import type { UnreleasedSearchHit } from '@shared/apiClient/enrichment';

import { useUnreleasedHitsView } from '@features/discovery/ports/UnreleasedHitsSlot';
import { SectionLabel } from '@features/discovery/ui/SectionLabel';

const UNRELEASED_SECTION_CAP = 5;

interface HitsProps {
  hits: UnreleasedSearchHit[];
}

export function UnreleasedSection({ hits }: HitsProps): ReactElement | null {
  const HitsView = useUnreleasedHitsView();
  if (hits.length === 0) return null;
  return (
    <View testID="discover-unreleased-section" style={styles.section}>
      <SectionLabel style={styles.label}>UNRELEASED</SectionLabel>
      <HitsView hits={hits} limit={UNRELEASED_SECTION_CAP} />
    </View>
  );
}

export function UnreleasedAllHits({ hits }: HitsProps): ReactElement {
  const HitsView = useUnreleasedHitsView();
  return (
    <ScrollView testID="discover-unreleased-all" contentContainerStyle={styles.all}>
      <HitsView hits={hits} />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  section: { marginBottom: spacing.xl },
  label: { marginBottom: spacing.sm, marginTop: spacing.sm },
  all: { paddingTop: spacing.sm, paddingBottom: spacing.xl },
});

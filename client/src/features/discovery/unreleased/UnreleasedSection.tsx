import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { spacing } from '@shared/ui';
import type { UnreleasedSearchHit } from '@shared/apiClient/enrichment';

import { UnreleasedHitsSlot } from '@features/discovery/ports/UnreleasedHitsSlot';
import { SectionLabel } from '@features/discovery/ui/SectionLabel';

const UNRELEASED_SECTION_CAP = 5;

interface UnreleasedSectionProps {
  hits: UnreleasedSearchHit[];
}

export function UnreleasedSection({ hits }: UnreleasedSectionProps): ReactElement | null {
  if (hits.length === 0) return null;
  return (
    <View testID="discover-unreleased-section" style={styles.section}>
      <SectionLabel style={styles.label}>UNRELEASED</SectionLabel>
      <UnreleasedHitsSlot hits={hits} limit={UNRELEASED_SECTION_CAP} />
    </View>
  );
}

const styles = StyleSheet.create({
  section: { marginBottom: spacing.xl },
  label: { marginBottom: spacing.sm, marginTop: spacing.sm },
});

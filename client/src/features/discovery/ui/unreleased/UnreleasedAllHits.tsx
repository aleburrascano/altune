import type { ReactElement } from 'react';
import { ScrollView, StyleSheet } from 'react-native';

import { spacing } from '@shared/ui';
import type { UnreleasedSearchHit } from '@shared/apiClient/enrichment';

import { UnreleasedHitsSlot } from '@features/discovery/ports/UnreleasedHitsSlot';

interface UnreleasedAllHitsProps {
  hits: UnreleasedSearchHit[];
}

export function UnreleasedAllHits({ hits }: UnreleasedAllHitsProps): ReactElement {
  return (
    <ScrollView testID="discover-unreleased-all" contentContainerStyle={styles.allHits}>
      <UnreleasedHitsSlot hits={hits} />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  allHits: { paddingTop: spacing.sm, paddingBottom: spacing.xl },
});

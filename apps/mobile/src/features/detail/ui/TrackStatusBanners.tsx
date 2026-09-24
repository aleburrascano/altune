import { type ReactElement } from 'react';
import { ActivityIndicator, StyleSheet, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

import type { LateralNavHandle } from '../hooks/useTrackDetailActions';
import type { SaveFailure } from '../hooks/useSaveTrack';
import { saveFailureBanner } from '../save-control-state';

function SearchingIndicator(): ReactElement {
  return (
    <View style={styles.lateralLoading}>
      <ActivityIndicator size="small" />
      <Text variant="label" tone="secondary">
        Searching...
      </Text>
    </View>
  );
}

export function TrackStatusBanners({
  saveFailure,
  lateralNav,
}: {
  saveFailure: SaveFailure | null;
  lateralNav: LateralNavHandle;
}): ReactElement {
  return (
    <>
      {saveFailure !== null ? (
        <Banner testID="detail-save-error" tone="danger" style={styles.banner}>
          {saveFailureBanner(saveFailure)}
        </Banner>
      ) : null}
      {lateralNav.error !== null ? (
        <Banner testID="detail-lateral-error" tone="danger" style={styles.banner}>
          {lateralNav.error}
        </Banner>
      ) : null}
      {lateralNav.state === 'searching' ? <SearchingIndicator /> : null}
    </>
  );
}

const styles = StyleSheet.create({
  banner: { marginTop: spacing.lg },
  lateralLoading: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.sm,
    marginTop: spacing.md,
  },
});

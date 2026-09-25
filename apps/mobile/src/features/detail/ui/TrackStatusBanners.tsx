import { useEffect, type ReactElement } from 'react';
import { ActivityIndicator, StyleSheet, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';
import { recordFailureShownOnce } from '@shared/acquisition/acquisitionTelemetry';

import type { LateralNavHandle } from '../hooks/useTrackDetailActions';
import type { SaveFailure } from '../hooks/useSaveTrack';
import { saveFailureBanner } from '../save-control-state';

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

const dangerBannerProps = { tone: 'danger' as const, style: styles.banner };

function SaveFailureBanner({ saveFailure }: { saveFailure: SaveFailure }): ReactElement {
  const { trackId, message } = saveFailure;
  useEffect(() => {
    if (trackId !== undefined) recordFailureShownOnce(trackId, message);
  }, [trackId, message]);
  return (
    <Banner testID="detail-save-error" {...dangerBannerProps}>
      {saveFailureBanner(saveFailure)}
    </Banner>
  );
}

function LateralErrorBanner({ error }: { error: string }): ReactElement {
  return (
    <Banner testID="detail-lateral-error" {...dangerBannerProps}>
      {error}
    </Banner>
  );
}

type TrackStatusBannersProps = { saveFailure: SaveFailure | null; lateralNav: LateralNavHandle };

export function TrackStatusBanners(props: TrackStatusBannersProps): ReactElement {
  const { saveFailure, lateralNav } = props;
  return (
    <>
      {saveFailure !== null ? <SaveFailureBanner saveFailure={saveFailure} /> : null}
      {lateralNav.error !== null ? <LateralErrorBanner error={lateralNav.error} /> : null}
      {lateralNav.state === 'searching' ? <SearchingIndicator /> : null}
    </>
  );
}

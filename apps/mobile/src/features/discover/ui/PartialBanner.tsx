/**
 * PartialBanner — small banner shown above results when any provider failed.
 *
 * Slice 46. testID = "discover-partial-banner". Does NOT replace the
 * results — it sits as a sibling so partial responses still show data.
 */

import type { ReactElement } from 'react';
import { StyleSheet, Text, View } from 'react-native';

import type { DiscoveryProviderInfo } from '../../../shared/api-client/discovery';

export type PartialBannerProps = {
  providers: DiscoveryProviderInfo[];
};

export function PartialBanner({ providers }: PartialBannerProps): ReactElement {
  const unavailable = providers
    .filter((p) => p.status !== 'ok')
    .map((p) => p.provider)
    .join(', ');
  return (
    <View style={styles.banner} testID="discover-partial-banner">
      <Text style={styles.text}>Some sources are unavailable: {unavailable}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  banner: {
    backgroundColor: '#3a2a00',
    paddingHorizontal: 16,
    paddingVertical: 6,
  },
  text: { color: '#ffcb6b', fontSize: 12 },
});

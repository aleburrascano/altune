/**
 * Single track row in the library list (ADR-0008 restyle).
 *
 * Title (primary) + artist (secondary), per spec AC#1. testID
 * `library-row-<track-id>` preserved. Album, duration, and album art are
 * deliberately absent in v1 — they earn their place in future specs.
 */

import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text, spacing, useTheme } from '@shared/ui';

import type { TrackResponse } from '../../../shared/api-client/types';

export function LibraryRow({ track }: { track: TrackResponse }): ReactElement {
  const theme = useTheme();
  return (
    <View
      testID={`library-row-${track.id}`}
      style={[styles.row, { borderBottomColor: theme.color.border }]}
    >
      <Text variant="bodyStrong" numberOfLines={1}>
        {track.title}
      </Text>
      <Text variant="label" tone="secondary" numberOfLines={1} style={styles.artist}>
        {track.artist}
      </Text>
      {track.acquisition_status === 'pending' ? (
        <Text
          testID={`library-row-pending-${track.id}`}
          variant="caption"
          tone="tertiary"
          style={styles.pending}
        >
          Pending
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    paddingVertical: spacing.md,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  artist: { marginTop: 2 },
  pending: { marginTop: spacing.xs },
});

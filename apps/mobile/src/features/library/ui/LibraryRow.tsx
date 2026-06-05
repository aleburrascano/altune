/**
 * Single track row in the library list (ADR-0008 restyle).
 *
 * Title (primary) + artist (secondary), per spec AC#1. testID
 * `library-row-<track-id>` preserved. Album, duration, and album art are
 * deliberately absent in v1 — they earn their place in future specs.
 */

import type { ReactElement } from 'react';
import { Pressable, StyleSheet } from 'react-native';

import { Text, spacing, useTheme } from '@shared/ui';

import type { TrackResponse } from '../../../shared/api-client/types';

type LibraryRowProps = {
  track: TrackResponse;
  onPress: () => void;
};

export function LibraryRow({ track, onPress }: LibraryRowProps): ReactElement {
  const theme = useTheme();
  const pendingLabel = track.acquisition_status === 'pending' ? ', pending' : '';
  const a11yLabel = `${track.title} by ${track.artist}${pendingLabel}`;

  return (
    <Pressable
      testID={`library-row-${track.id}`}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={a11yLabel}
      style={({ pressed }) => [
        styles.row,
        { borderBottomColor: theme.color.border },
        pressed ? styles.pressed : null,
      ]}
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
    </Pressable>
  );
}

const styles = StyleSheet.create({
  row: {
    paddingVertical: spacing.md,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  pressed: { opacity: 0.7 },
  artist: { marginTop: spacing.xs },
  pending: { marginTop: spacing.xs },
});

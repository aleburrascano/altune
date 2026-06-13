import type { ReactElement } from 'react';
import { Pressable, StyleSheet } from 'react-native';

import { formatDuration } from '@shared/lib/format';
import { Artwork, Row, Text, spacing, useTheme } from '@shared/ui';

import type { TrackResponse } from '../../../shared/api-client/types';

type LibraryRowProps = {
  track: TrackResponse;
  onPress: () => void;
  onLongPress?: () => void;
  onDelete?: () => void;
};

export function LibraryRow({ track, onPress, onLongPress }: LibraryRowProps): ReactElement {
  const theme = useTheme();
  const pendingLabel = track.acquisition_status === 'pending' ? ', pending' : '';
  const failedLabel = track.acquisition_status === 'failed' ? ', failed' : '';
  const albumLabel = track.album != null ? ` · ${track.album}` : '';
  const a11yLabel = `${track.title} by ${track.artist}${albumLabel}${pendingLabel}${failedLabel}`;

  const duration =
    track.duration_seconds != null && track.duration_seconds > 0
      ? formatDuration(track.duration_seconds)
      : null;

  return (
    <Pressable
      testID={`library-row-${track.id}`}
      onPress={onPress}
      onLongPress={onLongPress}
      delayLongPress={400}
      accessibilityRole="button"
      accessibilityLabel={a11yLabel}
      style={({ pressed }) => [
        styles.row,
        { borderBottomColor: theme.color.border },
        pressed ? styles.pressed : null,
      ]}
    >
      <Row
        leading={
          <Artwork uri={track.artwork_url} size={48} radius={6} accessibilityLabel="Album art" />
        }
        trailing={
          duration != null ? (
            <Text variant="caption" tone="tertiary">
              {duration}
            </Text>
          ) : null
        }
      >
        <Text variant="bodyStrong" numberOfLines={1}>
          {track.title}
        </Text>
        <Text variant="label" tone="secondary" numberOfLines={1} style={styles.subtitle}>
          {track.artist}
          {albumLabel}
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
        {track.acquisition_status === 'failed' ? (
          <Text
            testID={`library-row-failed-${track.id}`}
            variant="caption"
            tone="danger"
            style={styles.failed}
          >
            {track.failure_reason ?? 'Acquisition failed'}
          </Text>
        ) : null}
      </Row>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  row: {
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.lg,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  pressed: { opacity: 0.7 },
  subtitle: { marginTop: 2 },
  pending: { marginTop: 2 },
  failed: { marginTop: 2 },
});

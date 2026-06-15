import type { ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native';

import { formatDuration } from '@shared/lib/format';
import { Artwork, Row, Text, spacing, useTheme } from '@shared/ui';

import type { TrackResponse } from '../../../shared/api-client/types';
import { formatFailureReason } from './formatFailureReason';

type LibraryRowProps = {
  track: TrackResponse;
  onPress: () => void;
  onLongPress?: () => void;
  onDelete?: () => void;
  onRetry?: (() => void) | undefined;
  retrying?: boolean;
};

export function LibraryRow({ track, onPress, onLongPress, onRetry, retrying }: LibraryRowProps): ReactElement {
  const theme = useTheme();
  const pendingLabel = track.acquisition_status === 'pending' ? ', pending' : '';
  const retryLabel = retrying ? ', retrying' : onRetry != null ? ', retry available' : '';
  const failedLabel = track.acquisition_status === 'failed' ? ', failed' : '';
  const albumLabel = track.album != null ? ` · ${track.album}` : '';
  const a11yLabel = `${track.title} by ${track.artist}${albumLabel}${pendingLabel}${failedLabel}${retryLabel}`;

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
          <View style={styles.failedRow}>
            <Text
              testID={`library-row-failed-${track.id}`}
              variant="caption"
              tone="danger"
              style={styles.failed}
              numberOfLines={1}
            >
              {retrying ? 'Retrying…' : formatFailureReason(track.failure_reason)}
            </Text>
            {onRetry != null ? (
              retrying ? (
                <ActivityIndicator testID={`library-row-retrying-${track.id}`} size="small" color={theme.color.accent} />
              ) : (
                <Pressable
                  testID={`library-row-retry-${track.id}`}
                  onPress={(e) => {
                    e?.stopPropagation?.();
                    onRetry();
                  }}
                  hitSlop={8}
                  accessibilityRole="button"
                  accessibilityLabel={`Retry acquisition for ${track.title}`}
                >
                  <Text variant="caption" tone="accent">
                    Retry
                  </Text>
                </Pressable>
              )
            ) : null}
          </View>
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
  failedRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, marginTop: 2 },
  failed: { flexShrink: 1 },
});

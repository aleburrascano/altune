import { useRef, type ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, useWindowDimensions, View } from 'react-native';

import { MoreVertical } from 'lucide-react-native';

import { stageLabel } from '@shared/acquisition/stagePhase';
import { useTrackStage } from '@shared/acquisition/stageStore';
import { formatDuration } from '@shared/lib/format';
import { Artwork, Row, Text, spacing, useTheme } from '@shared/ui';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import type { TrackResponse } from '../../../shared/api-client/types';
import { formatFailureReason } from './formatFailureReason';

type LibraryRowProps = {
  track: TrackResponse;
  onPlay?: () => void;
  onPress: () => void;
  onMore: (anchor: MenuAnchor) => void;
  onRetry?: (() => void) | undefined;
  retrying?: boolean;
  isPlaying?: boolean;
};

export function LibraryRow({ track, onPlay, onPress, onMore, onRetry, retrying, isPlaying }: LibraryRowProps): ReactElement {
  const theme = useTheme();
  const moreRef = useRef<View>(null);
  const { width: windowWidth } = useWindowDimensions();

  // Measure the ⋮ button so the menu floats anchored to it (measureInWindow
  // gives window coordinates; right offset = distance from the window's right
  // edge). A null ref (not yet laid out) just skips opening.
  const handleMore = () => {
    const node = moreRef.current;
    if (node == null) return;
    node.measureInWindow((x, y, width, height) => {
      onMore({ top: y, bottom: y + height, right: windowWidth - (x + width) });
    });
  };
  const stage = useTrackStage(track.id);
  const isReady = track.acquisition_status === 'ready';
  const pendingLabel = track.acquisition_status === 'pending' ? ', pending' : '';
  const retryLabel = retrying ? ', retrying' : onRetry != null ? ', retry available' : '';
  const failedLabel = track.acquisition_status === 'failed' ? ', failed' : '';
  const albumLabel = track.album != null ? ` · ${track.album}` : '';
  const a11yLabel = `${track.title} by ${track.artist}${albumLabel}${pendingLabel}${failedLabel}${retryLabel}`;

  const duration =
    track.duration_seconds != null && track.duration_seconds > 0
      ? formatDuration(track.duration_seconds)
      : null;

  const handlePress = () => {
    if (isReady && onPlay) {
      onPlay();
    } else {
      onPress();
    }
  };

  return (
    <Pressable
      testID={`library-row-${track.id}`}
      onPress={handlePress}
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
          <View style={styles.trailing}>
            {duration != null ? (
              <Text variant="caption" tone="tertiary">
                {duration}
              </Text>
            ) : null}
            <Pressable
              ref={moreRef}
              testID={`library-row-more-${track.id}`}
              onPress={(e) => { e.stopPropagation?.(); handleMore(); }}
              hitSlop={8}
              accessibilityRole="button"
              accessibilityLabel={`More options for ${track.title}`}
              style={styles.moreBtn}
            >
              <MoreVertical size={18} color={theme.color.textTertiary} />
            </Pressable>
          </View>
        }
      >
        <Text variant="bodyStrong" numberOfLines={1} {...(isPlaying ? { tone: 'accent' as const } : {})}>
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
            {stage != null ? stageLabel(stage) : 'Pending'}
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
  trailing: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  moreBtn: {
    minWidth: 44,
    minHeight: 44,
    alignItems: 'center',
    justifyContent: 'center',
  },
});

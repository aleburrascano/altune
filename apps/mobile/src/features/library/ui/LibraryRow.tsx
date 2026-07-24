import { memo, useRef, type ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, useWindowDimensions, View } from 'react-native';

import { ArrowDownCircle, CircleCheck, MoreVertical } from 'lucide-react-native';

import { useDownloadPhase } from '@shared/acquisition/downloadStore';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { phaseLabel } from '@shared/acquisition/stagePhase';
import { withFeaturing } from '@shared/lib/featured';
import { formatDuration } from '@shared/lib/format';
import { Row, Text, spacing, useTheme } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';
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

function LibraryRowImpl({ track, onPlay, onPress, onMore, onRetry, retrying, isPlaying }: LibraryRowProps): ReactElement {
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
  const phase = useDownloadPhase(track.id);
  // Offline availability, distinct from acquisition: `phase` is the SERVER
  // fetching the audio, this is a copy on THIS device.
  const pinned = usePinnedStore((s) => s.entries[track.id]?.status);
  const isReady = track.acquisition_status === 'ready';
  const pendingLabel = track.acquisition_status === 'pending' ? ', pending' : '';
  const retryLabel = retrying ? ', retrying' : onRetry != null ? ', retry available' : '';
  const failedLabel = track.acquisition_status === 'failed' ? ', failed' : '';
  const albumLabel = track.album != null ? ` · ${track.album}` : '';
  const offlineLabel =
    pinned === 'ready' ? ', downloaded' : pinned === 'downloading' || pinned === 'queued' ? ', downloading' : '';
  const a11yLabel = `${track.title} by ${track.artist}${albumLabel}${pendingLabel}${failedLabel}${retryLabel}${offlineLabel}`;

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
            {pinned === 'ready' ? (
              <CircleCheck
                testID={`library-row-offline-${track.id}`}
                size={14}
                color={theme.color.accent}
              />
            ) : pinned === 'downloading' || pinned === 'queued' ? (
              <ArrowDownCircle
                testID={`library-row-offline-pending-${track.id}`}
                size={14}
                color={theme.color.textTertiary}
              />
            ) : null}
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
          {withFeaturing(track.artist, track.featured_artists)}
          {albumLabel}
        </Text>
        {phase != null && phase !== 'failed' ? (
          // A live download phase shows regardless of cache status (F7) — so
          // re-acquire/retry/recovery of a ready/failed track shows progress too,
          // not only a fresh 'pending' row.
          <Text
            testID={`library-row-pending-${track.id}`}
            variant="caption"
            tone="tertiary"
            style={styles.pending}
          >
            {phaseLabel(phase)}
          </Text>
        ) : track.acquisition_status === 'pending' ? (
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
  trailing: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  moreBtn: {
    minWidth: 44,
    minHeight: 44,
    alignItems: 'center',
    justifyContent: 'center',
  },
});

/**
 * Rows are memoized on their DATA only.
 *
 * The list's `renderItem` mints fresh `onPlay`/`onPress`/`onMore`/`onRetry`
 * closures on every parent render, so the default shallow compare never hits —
 * every row re-renders (and re-measures its Artwork) when any unrelated screen
 * state changes, e.g. a keystroke in the search bar. Those closures are
 * semantically identical between renders: each is `() => handler(item)` over the
 * same item. Comparing the rendered fields instead is what makes the memo real.
 *
 * The cost: if a handler's *behaviour* ever starts depending on state not listed
 * here, the row keeps the stale closure. Anything a handler reads must therefore
 * be either passed as a compared prop or read from a store at call time.
 */
export const LibraryRow = memo(LibraryRowImpl, (prev, next) => {
  const a = prev.track;
  const b = next.track;
  return (
    a.id === b.id &&
    a.title === b.title &&
    a.artist === b.artist &&
    a.album === b.album &&
    a.artwork_url === b.artwork_url &&
    a.duration_seconds === b.duration_seconds &&
    a.acquisition_status === b.acquisition_status &&
    a.failure_reason === b.failure_reason &&
    prev.retrying === next.retrying &&
    prev.isPlaying === next.isPlaying &&
    (prev.onPlay == null) === (next.onPlay == null) &&
    (prev.onRetry == null) === (next.onRetry == null)
  );
});

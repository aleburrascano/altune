import { useCallback, type ReactElement } from 'react';
import { Alert, FlatList, type ListRenderItemInfo, Pressable, StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { ChevronDown, Play, X } from 'lucide-react-native';
import ReanimatedSwipeable from 'react-native-gesture-handler/ReanimatedSwipeable';
import Reanimated, { type SharedValue, useAnimatedStyle } from 'react-native-reanimated';

import { withFeaturing } from '@shared/lib/featured';
import type { FeaturedArtist } from '@shared/api-client/types';
import { useQueueStore } from '@shared/playback/queueStore';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { Text } from '@shared/ui/primitives/Text';
import type { Theme } from '@shared/ui/theme';
import { useTheme } from '@shared/ui/theme';
import { fontFamily, radius, spacing } from '@shared/ui/theme/tokens';

type QueueItem = {
  trackIndex: number;
  queueIndex: number;
  title: string;
  artist: string;
  artworkUrl: string | null;
  durationSeconds: number | undefined;
  featuredArtists: readonly FeaturedArtist[] | undefined;
};

function formatTime(sec: number | undefined): string {
  if (sec == null || sec === 0) return '';
  const m = Math.floor(sec / 60);
  const s = Math.floor(sec % 60);
  return `${m}:${String(s).padStart(2, '0')}`;
}

function RemoveAction(_prog: SharedValue<number>, drag: SharedValue<number>, theme: Theme) {
  const style = useAnimatedStyle(() => ({
    transform: [{ translateX: drag.value + 80 }],
  }));
  return (
    <Reanimated.View style={[styles.removeAction, { backgroundColor: theme.color.danger }, style]}>
      <Text variant="label" style={{ color: theme.color.onAccent }}>Remove</Text>
    </Reanimated.View>
  );
}

export function QueueSheet(): ReactElement {
  const theme = useTheme();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const tracks = useQueueStore((s) => s.tracks);
  const playOrder = useQueueStore((s) => s.playOrder);
  const currentIndex = useQueueStore((s) => s.currentIndex);
  const source = useQueueStore((s) => s.source);
  const { skipToIndex, removeFromQueue, clearUpcoming } = useQueuePlayback();

  const sourceLabel = source
    ? source.kind === 'playlist' ? `Playing from ${source.name}`
      : source.kind === 'library' ? 'Playing from Library'
        : 'Playing from search'
    : 'Queue';

  const currentTrackData = currentIndex >= 0 && currentIndex < playOrder.length
    ? tracks[playOrder[currentIndex]!]
    : null;

  const upNextItems: QueueItem[] = [];
  for (let i = currentIndex + 1; i < playOrder.length; i++) {
    const trackIdx = playOrder[i];
    if (trackIdx == null) continue;
    const t = tracks[trackIdx];
    if (!t) continue;
    upNextItems.push({
      trackIndex: trackIdx,
      queueIndex: i,
      title: t.title,
      artist: t.artist,
      artworkUrl: t.artworkUrl,
      durationSeconds: t.durationSeconds,
      featuredArtists: t.featuredArtists,
    });
  }

  const handleClear = () => {
    Alert.alert('Clear Queue', 'Remove all upcoming tracks?', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Clear',
        style: 'destructive',
        // clearUpcoming reads the queue at press time (this Alert is modal and can
        // sit open across auto-advances) and keeps the store and native queue in
        // lockstep — see useQueuePlayback.
        onPress: clearUpcoming,
      },
    ]);
  };

  const renderUpNextItem = useCallback(
    ({ item }: ListRenderItemInfo<QueueItem>) => (
      <ReanimatedSwipeable
        friction={2}
        rightThreshold={40}
        renderRightActions={(prog, drag) => RemoveAction(prog, drag, theme)}
        onSwipeableOpen={() => removeFromQueue(item.queueIndex)}
      >
        <Pressable
          onPress={() => skipToIndex(item.queueIndex)}
          style={[styles.row, { backgroundColor: theme.color.canvas }]}
          accessibilityRole="button"
          accessibilityLabel={`${item.title} by ${item.artist}`}
        >
          <Artwork uri={item.artworkUrl} size={40} radius={radius.sm} />
          <View style={styles.rowInfo}>
            <Text variant="label" numberOfLines={1}>{item.title}</Text>
            <Text variant="caption" tone="secondary" numberOfLines={1}>{withFeaturing(item.artist, item.featuredArtists)}</Text>
          </View>
          <Text variant="caption" tone="tertiary">{formatTime(item.durationSeconds)}</Text>
          <IconButton
            icon={X}
            size={18}
            onPress={() => removeFromQueue(item.queueIndex)}
            accessibilityLabel={`Remove ${item.title} from queue`}
          />
        </Pressable>
      </ReanimatedSwipeable>
    ),
    [theme, removeFromQueue, skipToIndex],
  );

  return (
    <View style={[styles.container, { backgroundColor: theme.color.canvas, paddingTop: insets.top }]}>
      {/* Header */}
      <View style={styles.header}>
        <IconButton
          icon={ChevronDown}
          size={28}
          onPress={() => router.back()}
          accessibilityLabel="Close queue"
        />
        <View style={styles.headerCenter}>
          <Text variant="title">Up Next</Text>
          <Text variant="caption" tone="secondary">{sourceLabel}</Text>
        </View>
        {upNextItems.length > 0 ? (
          <Pressable onPress={handleClear} hitSlop={8} accessibilityRole="button" accessibilityLabel="Clear queue">
            <Text variant="caption" style={{ color: theme.color.danger }}>Clear</Text>
          </Pressable>
        ) : (
          <View style={styles.headerSpacer} />
        )}
      </View>

      {/* Now Playing */}
      {currentTrackData ? (
        <View style={[styles.nowPlaying, { backgroundColor: theme.color.surface1 }]}>
          <View style={styles.nowPlayingContent}>
            <View style={styles.artworkWrap}>
              <Artwork uri={currentTrackData.artworkUrl} size={48} radius={radius.sm} />
              <View style={[styles.playBadge, { backgroundColor: theme.color.accent }]}>
                <Play size={8} color={theme.color.onAccent} fill={theme.color.onAccent} />
              </View>
            </View>
            <View style={styles.nowPlayingInfo}>
              <Text variant="caption" tone="accent" style={styles.nowPlayingLabel}>NOW PLAYING</Text>
              <Text variant="bodyStrong" numberOfLines={1}>{currentTrackData.title}</Text>
              <Text variant="caption" tone="secondary" numberOfLines={1}>{withFeaturing(currentTrackData.artist, currentTrackData.featuredArtists)}</Text>
            </View>
            <Text variant="caption" tone="tertiary">{formatTime(currentTrackData.durationSeconds)}</Text>
          </View>
        </View>
      ) : null}

      {/* Up Next */}
      {upNextItems.length > 0 ? (
        <View style={styles.sectionHeader}>
          <Text variant="caption" tone="secondary" style={styles.sectionLabel}>
            UP NEXT · {upNextItems.length} {upNextItems.length === 1 ? 'track' : 'tracks'}
          </Text>
        </View>
      ) : null}

      <FlatList
        data={upNextItems}
        // Key by the entry's slot in `tracks`, not its play-order position:
        // queueIndex shifts down on every advance and removal, so React
        // reconciles a different track onto the same key — remounting every row
        // (and its swipeable's gesture handler) on each transition, and leaving
        // the row that inherits a removed row's key rendered swiped-open.
        // trackIndex is unique across the queue and stable across advances.
        keyExtractor={(item) => `${item.trackIndex}`}
        renderItem={renderUpNextItem}
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.list}
        ListEmptyComponent={
          <View style={styles.empty}>
            <Text variant="label" tone="secondary">No upcoming tracks</Text>
          </View>
        }
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1 },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: spacing.lg,
    paddingBottom: spacing.sm,
  },
  headerCenter: { alignItems: 'center' },
  headerSpacer: { width: 44 },
  nowPlaying: {
    marginHorizontal: spacing.lg,
    borderRadius: radius.md,
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.md,
  },
  nowPlayingContent: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
  },
  artworkWrap: { position: 'relative' },
  playBadge: {
    position: 'absolute',
    bottom: -2,
    right: -2,
    width: 16,
    height: 16,
    borderRadius: 8,
    alignItems: 'center',
    justifyContent: 'center',
  },
  nowPlayingInfo: { flex: 1, gap: 1 },
  nowPlayingLabel: {
    textTransform: 'uppercase',
    letterSpacing: 0.5,
    fontFamily: fontFamily.bodySemiBold,
  },
  sectionHeader: {
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.lg,
    paddingBottom: spacing.xs,
  },
  sectionLabel: {
    textTransform: 'uppercase',
    letterSpacing: 0.5,
    fontFamily: fontFamily.bodySemiBold,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    gap: spacing.sm,
  },
  rowInfo: { flex: 1, gap: 2 },
  removeAction: {
    justifyContent: 'center',
    alignItems: 'center',
    width: 80,
  },
  list: { paddingBottom: spacing['3xl'] },
  empty: {
    alignItems: 'center',
    paddingTop: spacing['3xl'],
  },
});

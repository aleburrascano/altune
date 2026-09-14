import { useCallback, useState, type ReactElement } from 'react';
import { FlatList, type ListRenderItemInfo, Pressable, StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import { Play } from 'lucide-react-native';

import { countLabel } from '@shared/lib/format';
import { withFeaturing } from '@shared/lib/featured';
import { useQueueStore } from '@shared/playback/queueStore';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { confirmDestructive } from '@shared/ui/confirmDestructive';
import { ActionSheet } from '@shared/ui/primitives/ActionSheet';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';
import { fontFamily, radius, spacing } from '@shared/ui/theme/tokens';

import { queueMenuOptions } from '../queueMenuOptions';
import { formatTime, type QueueItem } from '../queueItem';
import { QueueRow } from './QueueRow';
import { SheetHeader, SheetHeaderCenter, SheetHeaderTrailing, SheetScreen } from './SheetHeader';

export function QueueSheet(): ReactElement {
  const theme = useTheme();
  const router = useRouter();
  const tracks = useQueueStore((s) => s.tracks);
  const playOrder = useQueueStore((s) => s.playOrder);
  const currentIndex = useQueueStore((s) => s.currentIndex);
  const source = useQueueStore((s) => s.source);
  const { skipToIndex, removeFromQueue, moveQueueItem, clearUpcoming } = useQueuePlayback();
  const [menuItem, setMenuItem] = useState<QueueItem | null>(null);

  const sourceLabel = source
    ? source.kind === 'playlist'
      ? `Playing from ${source.name}`
      : source.kind === 'library'
        ? 'Playing from Library'
        : 'Playing from search'
    : 'Queue';

  const currentTrackData =
    currentIndex >= 0 && currentIndex < playOrder.length ? tracks[playOrder[currentIndex]!] : null;

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
    confirmDestructive({
      title: 'Clear Queue',
      message: 'Remove all upcoming tracks?',
      confirmLabel: 'Clear',
      onConfirm: clearUpcoming,
    });
  };

  const renderUpNextItem = useCallback(
    ({ item }: ListRenderItemInfo<QueueItem>) => (
      <QueueRow
        item={item}
        onSkip={skipToIndex}
        onRemove={removeFromQueue}
        onOpenMenu={setMenuItem}
      />
    ),
    [removeFromQueue, skipToIndex],
  );

  return (
    <SheetScreen>
      <SheetHeader onClose={() => router.back()} closeLabel="Close queue">
        <SheetHeaderCenter>
          <Text variant="title">Up Next</Text>
          <Text variant="caption" tone="secondary">
            {sourceLabel}
          </Text>
        </SheetHeaderCenter>
        <SheetHeaderTrailing>
          {upNextItems.length > 0 ? (
            <Pressable
              onPress={handleClear}
              hitSlop={8}
              accessibilityRole="button"
              accessibilityLabel="Clear queue"
            >
              <Text variant="caption" style={{ color: theme.color.danger }}>
                Clear
              </Text>
            </Pressable>
          ) : null}
        </SheetHeaderTrailing>
      </SheetHeader>

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
              <Text variant="caption" tone="accent" style={styles.nowPlayingLabel}>
                NOW PLAYING
              </Text>
              <Text variant="bodyStrong" numberOfLines={1}>
                {currentTrackData.title}
              </Text>
              <Text variant="caption" tone="secondary" numberOfLines={1}>
                {withFeaturing(currentTrackData.artist, currentTrackData.featuredArtists)}
              </Text>
            </View>
            <Text variant="caption" tone="tertiary">
              {formatTime(currentTrackData.durationSeconds)}
            </Text>
          </View>
        </View>
      ) : null}

      {upNextItems.length > 0 ? (
        <View style={styles.sectionHeader}>
          <Text variant="caption" tone="secondary" style={styles.sectionLabel}>
            UP NEXT · {upNextItems.length} {countLabel(upNextItems.length, 'track')}
          </Text>
        </View>
      ) : null}

      <FlatList
        data={upNextItems}
        keyExtractor={(item) => `${item.trackIndex}`}
        renderItem={renderUpNextItem}
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.list}
        ListEmptyComponent={
          <View style={styles.empty}>
            <Text variant="label" tone="secondary">
              No upcoming tracks
            </Text>
          </View>
        }
      />

      <ActionSheet
        visible={menuItem != null}
        title={menuItem?.title}
        subtitle={
          menuItem != null ? withFeaturing(menuItem.artist, menuItem.featuredArtists) : undefined
        }
        options={
          menuItem != null
            ? queueMenuOptions(menuItem.queueIndex, {
                currentIndex,
                queueLength: playOrder.length,
                moveQueueItem,
                removeFromQueue,
              })
            : []
        }
        onClose={() => setMenuItem(null)}
      />
    </SheetScreen>
  );
}

const styles = StyleSheet.create({
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
  list: { paddingBottom: spacing['3xl'] },
  empty: {
    alignItems: 'center',
    paddingTop: spacing['3xl'],
  },
});

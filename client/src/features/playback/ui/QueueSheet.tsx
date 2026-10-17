import { useCallback, useState, type ReactElement } from 'react';
import { FlatList, type ListRenderItemInfo, StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import { EllipsisVertical, Play } from 'lucide-react-native';
import ReanimatedSwipeable from 'react-native-gesture-handler/ReanimatedSwipeable';
import Reanimated, { type SharedValue, useAnimatedStyle } from 'react-native-reanimated';

import { countLabel } from '@shared/ui/format/displayFormat';
import { withFeaturing } from '@shared/apiClient/featuredArtists';
import { useQueueStore } from '@shared/playback/queueStore';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { confirmDestructive } from '@shared/ui/dialog/dialog';
import { ActionSheet } from '@shared/ui/primitives/ActionSheet';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { Text } from '@shared/ui/primitives/Text';
import type { Theme } from '@shared/ui/theme';
import { useTheme, halved } from '@shared/ui/theme';
import { fontFamily, radius, spacing } from '@shared/ui/theme/tokens';

import {
  buildUpNextItems,
  formatSeconds,
  queueMenuOptions,
  queueSourceLabel,
  type QueueItem,
} from '@features/playback/domain/upNextQueue';
import {
  SheetHeader,
  SheetHeaderCenter,
  SheetHeaderTrailing,
  SheetScreen,
} from '@features/playback/ui/SheetHeader';

const REMOVE_ACTION_WIDTH = 80;

type RemoveActionProps = { drag: SharedValue<number>; theme: Theme };

function useRemoveActionStyle(drag: SharedValue<number>) {
  return useAnimatedStyle(() => ({
    transform: [{ translateX: drag.value + REMOVE_ACTION_WIDTH }],
  }));
}

type RemoveLabelProps = { color: string };

function RemoveLabel({ color }: RemoveLabelProps): ReactElement {
  return (
    <Text variant="label" style={{ color }}>
      Remove
    </Text>
  );
}

function RemoveAction({ drag, theme }: RemoveActionProps): ReactElement {
  const style = useRemoveActionStyle(drag);
  const background = { backgroundColor: theme.color.danger };
  return (
    <Reanimated.View style={[rowStyles.removeAction, background, style]}>
      <RemoveLabel color={theme.color.onAccent} />
    </Reanimated.View>
  );
}

type QueueRowProps = {
  item: QueueItem;
  onSkip: (queueIndex: number) => void;
  onRemove: (queueIndex: number) => void;
  onOpenMenu: (item: QueueItem) => void;
};

export function QueueRow({ item, onSkip, onRemove, onOpenMenu }: QueueRowProps): ReactElement {
  const theme = useTheme();
  return (
    <ReanimatedSwipeable
      friction={2}
      rightThreshold={40}
      renderRightActions={(_prog, drag) => <RemoveAction drag={drag} theme={theme} />}
      onSwipeableOpen={() => onRemove(item.queueIndex)}
    >
      <Tappable
        action="playback.queue.skip_to"
        onPress={() => onSkip(item.queueIndex)}
        style={[rowStyles.row, { backgroundColor: theme.color.canvas }]}
        accessibilityRole="button"
        accessibilityLabel={`${item.title} by ${item.artist}`}
      >
        <Artwork uri={item.artworkUrl} size={40} radius={radius.sm} />
        <View style={rowStyles.rowInfo}>
          <Text variant="label" numberOfLines={1}>
            {item.title}
          </Text>
          <Text variant="caption" tone="secondary" numberOfLines={1}>
            {withFeaturing(item.artist, item.featuredArtists)}
          </Text>
        </View>
        <Text variant="caption" tone="tertiary">
          {formatSeconds(item.durationSeconds)}
        </Text>
        <IconButton
          action="playback.queue.row_menu"
          icon={EllipsisVertical}
          size={18}
          onPress={() => onOpenMenu(item)}
          accessibilityLabel={`Options for ${item.title}`}
        />
      </Tappable>
    </ReanimatedSwipeable>
  );
}

const PLAY_BADGE_SIZE = 16;
const SECTION_LABEL_LETTER_SPACING = 0.5;

const rowStyles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    gap: spacing.sm,
  },
  rowInfo: { flex: 1, gap: spacing['2xs'] },
  removeAction: {
    justifyContent: 'center',
    alignItems: 'center',
    width: REMOVE_ACTION_WIDTH,
  },
});

export function QueueSheet(): ReactElement {
  const theme = useTheme();
  const router = useRouter();
  const tracks = useQueueStore((s) => s.tracks);
  const playOrder = useQueueStore((s) => s.playOrder);
  const currentIndex = useQueueStore((s) => s.currentIndex);
  const source = useQueueStore((s) => s.source);
  const { skipToIndex, removeFromQueue, moveQueueItem, clearUpcoming } = useQueuePlayback();
  const [menuItem, setMenuItem] = useState<QueueItem | null>(null);

  const sourceLabel = queueSourceLabel(source);

  const currentTrackData =
    currentIndex >= 0 && currentIndex < playOrder.length ? tracks[playOrder[currentIndex]!] : null;

  const upNextItems = buildUpNextItems(tracks, playOrder, currentIndex);

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
            <Tappable
              action="playback.queue.clear"
              onPress={handleClear}
              hitSlop={8}
              accessibilityRole="button"
              accessibilityLabel="Clear queue"
            >
              <Text variant="caption" style={{ color: theme.color.danger }}>
                Clear
              </Text>
            </Tappable>
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
              {formatSeconds(currentTrackData.durationSeconds)}
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
    bottom: -spacing['2xs'],
    right: -spacing['2xs'],
    width: PLAY_BADGE_SIZE,
    height: PLAY_BADGE_SIZE,
    borderRadius: halved(PLAY_BADGE_SIZE),
    alignItems: 'center',
    justifyContent: 'center',
  },
  nowPlayingInfo: { flex: 1, gap: 1 },
  nowPlayingLabel: {
    textTransform: 'uppercase',
    letterSpacing: SECTION_LABEL_LETTER_SPACING,
    fontFamily: fontFamily.bodySemiBold,
  },
  sectionHeader: {
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.lg,
    paddingBottom: spacing.xs,
  },
  sectionLabel: {
    textTransform: 'uppercase',
    letterSpacing: SECTION_LABEL_LETTER_SPACING,
    fontFamily: fontFamily.bodySemiBold,
  },
  list: { paddingBottom: spacing['3xl'] },
  empty: {
    alignItems: 'center',
    paddingTop: spacing['3xl'],
  },
});

import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { EllipsisVertical } from 'lucide-react-native';
import ReanimatedSwipeable from 'react-native-gesture-handler/ReanimatedSwipeable';
import Reanimated, { type SharedValue, useAnimatedStyle } from 'react-native-reanimated';

import { withFeaturing } from '@shared/lib/featured';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { Text } from '@shared/ui/primitives/Text';
import type { Theme } from '@shared/ui/theme';
import { useTheme } from '@shared/ui/theme';
import { radius, spacing } from '@shared/ui/theme/tokens';

import { formatTime, type QueueItem } from '../queueItem';

function RemoveAction(_prog: SharedValue<number>, drag: SharedValue<number>, theme: Theme) {
  const style = useAnimatedStyle(() => ({
    transform: [{ translateX: drag.value + 80 }],
  }));
  return (
    <Reanimated.View style={[styles.removeAction, { backgroundColor: theme.color.danger }, style]}>
      <Text variant="label" style={{ color: theme.color.onAccent }}>
        Remove
      </Text>
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
      renderRightActions={(prog, drag) => RemoveAction(prog, drag, theme)}
      onSwipeableOpen={() => onRemove(item.queueIndex)}
    >
      <Pressable
        onPress={() => onSkip(item.queueIndex)}
        style={[styles.row, { backgroundColor: theme.color.canvas }]}
        accessibilityRole="button"
        accessibilityLabel={`${item.title} by ${item.artist}`}
      >
        <Artwork uri={item.artworkUrl} size={40} radius={radius.sm} />
        <View style={styles.rowInfo}>
          <Text variant="label" numberOfLines={1}>
            {item.title}
          </Text>
          <Text variant="caption" tone="secondary" numberOfLines={1}>
            {withFeaturing(item.artist, item.featuredArtists)}
          </Text>
        </View>
        <Text variant="caption" tone="tertiary">
          {formatTime(item.durationSeconds)}
        </Text>
        <IconButton
          icon={EllipsisVertical}
          size={18}
          onPress={() => onOpenMenu(item)}
          accessibilityLabel={`Options for ${item.title}`}
        />
      </Pressable>
    </ReanimatedSwipeable>
  );
}

const styles = StyleSheet.create({
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
});

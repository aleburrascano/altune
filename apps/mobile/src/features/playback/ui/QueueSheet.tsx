import { FlatList, Pressable, StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { ChevronDown } from 'lucide-react-native';

import { useQueueStore } from '@shared/playback/queueStore';
import { usePlayback } from '../hooks/usePlayback';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';
import { radius, spacing } from '@shared/ui/theme/tokens';

export function QueueSheet() {
  const theme = useTheme();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const tracks = useQueueStore((s) => s.tracks);
  const playOrder = useQueueStore((s) => s.playOrder);
  const currentIndex = useQueueStore((s) => s.currentIndex);
  const source = useQueueStore((s) => s.source);
  const playback = usePlayback();

  const sourceLabel = source
    ? source.kind === 'playlist' ? `Playing from ${source.name}`
      : source.kind === 'library' ? 'Playing from Library'
        : `Playing from search`
    : 'Queue';

  return (
    <View style={[styles.container, { backgroundColor: theme.color.canvas, paddingTop: insets.top }]}>
      <View style={styles.header}>
        <IconButton
          icon={ChevronDown}
          size={28}
          onPress={() => router.back()}
          accessibilityLabel="Close queue"
        />
        <Text variant="title">Up Next</Text>
        <View style={styles.headerSpacer} />
      </View>
      <Text variant="caption" tone="secondary" style={styles.sourceLabel}>
        {sourceLabel}
      </Text>
      <FlatList
        data={playOrder.map((trackIdx, queueIdx) => ({
          track: tracks[trackIdx]!,
          queueIndex: queueIdx,
          isCurrent: queueIdx === currentIndex,
        }))}
        keyExtractor={(_item, i) => `${i}`}
        renderItem={({ item }) => (
          <Pressable
            onPress={() => {
              const t = useQueueStore.getState().skipToIndex(item.queueIndex);
              if (t) playback.play(t);
            }}
            style={[styles.row, item.isCurrent ? { backgroundColor: theme.color.surface1 } : null]}
            accessibilityRole="button"
            accessibilityLabel={`${item.track.title} by ${item.track.artist}`}
          >
            <Artwork uri={item.track.artworkUrl} size={40} radius={radius.sm} />
            <View style={styles.rowInfo}>
              <Text
                variant="label"
                tone={item.isCurrent ? 'accent' : 'primary'}
                numberOfLines={1}
              >
                {item.track.title}
              </Text>
              <Text variant="caption" tone="secondary" numberOfLines={1}>
                {item.track.artist}
              </Text>
            </View>
          </Pressable>
        )}
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.list}
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
  headerSpacer: { width: 44 },
  sourceLabel: {
    paddingHorizontal: spacing.lg,
    paddingBottom: spacing.md,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    gap: spacing.md,
  },
  rowInfo: { flex: 1, gap: 2 },
  list: { paddingBottom: spacing['3xl'] },
});

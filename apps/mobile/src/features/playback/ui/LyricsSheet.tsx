import { useEffect, useRef, type ReactElement } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { ChevronDown } from 'lucide-react-native';

import { withFeaturing } from '@shared/lib/featured';
import { usePlayback } from '@shared/playback/usePlayback';
import { Text } from '@shared/ui/primitives/Text';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { Skeleton } from '@shared/ui/primitives/Skeleton';
import { useReduceMotion } from '@shared/ui/motion/useReduceMotion';
import { useTheme } from '@shared/ui/theme';
import { spacing } from '@shared/ui/theme/tokens';

import { useLyrics } from '../hooks/useLyrics';
import { activeLineIndex, _lyricsView } from '../lyrics-sync';

const SCROLL_LEAD_PX = 140;

export function LyricsSheet(): ReactElement {
  const router = useRouter();
  const theme = useTheme();
  const insets = useSafeAreaInsets();
  const reduceMotion = useReduceMotion();
  const { track, positionMs, seekTo } = usePlayback();

  const query = useLyrics(track ? { title: track.title, artist: track.artist } : null);
  const synced = query.data?.synced_lines ?? [];
  const plain = query.data?.plain ?? '';

  const view = _lyricsView({
    isLoading: query.isLoading,
    isError: query.isError,
    plain,
    syncedCount: synced.length,
  });

  const activeIndex = view === 'synced' ? activeLineIndex(synced, positionMs) : -1;

  const scrollRef = useRef<ScrollView | null>(null);
  const lineOffsets = useRef<Record<number, number>>({});
  useEffect(() => {
    if (activeIndex < 0) return;
    const y = lineOffsets.current[activeIndex];
    if (y === undefined) return;
    scrollRef.current?.scrollTo({ y: Math.max(0, y - SCROLL_LEAD_PX), animated: !reduceMotion });
  }, [activeIndex, reduceMotion]);

  return (
    <View
      testID="lyrics-sheet"
      style={[styles.container, { backgroundColor: theme.color.canvas, paddingTop: insets.top }]}
    >
      <View style={styles.header}>
        <IconButton
          icon={ChevronDown}
          size={28}
          onPress={() => router.back()}
          accessibilityLabel="Close lyrics"
        />
        <View style={styles.headerCenter}>
          <Text variant="title" numberOfLines={1}>
            {track?.title ?? 'Lyrics'}
          </Text>
          {track ? (
            <Text variant="caption" tone="secondary" numberOfLines={1}>
              {withFeaturing(track.artist, track.featuredArtists)}
            </Text>
          ) : null}
        </View>
        <View style={styles.headerSpacer} />
      </View>

      {view === 'loading' ? (
        <View testID="lyrics-loading" style={styles.states}>
          {[0, 1, 2, 3, 4, 5].map((i) => (
            <Skeleton key={i} width={i % 3 === 0 ? '60%' : '85%'} height={20} radius={4} />
          ))}
        </View>
      ) : null}

      {view === 'error' ? (
        <View testID="lyrics-error" style={styles.centered}>
          <Text variant="body" tone="secondary" style={styles.centeredText}>
            Couldn&apos;t load lyrics.
          </Text>
        </View>
      ) : null}

      {view === 'unavailable' ? (
        <View testID="lyrics-unavailable" style={styles.centered}>
          <Text variant="body" tone="secondary" style={styles.centeredText}>
            No lyrics for this track.
          </Text>
        </View>
      ) : null}

      {view === 'synced' ? (
        <ScrollView
          testID="lyrics-synced"
          ref={scrollRef}
          showsVerticalScrollIndicator={false}
          contentContainerStyle={styles.content}
        >
          {synced.map((line, index) => (
            <Pressable
              key={`${line.milliseconds}-${index}`}
              testID={`lyrics-line-${index}`}
              onLayout={(e) => {
                lineOffsets.current[index] = e.nativeEvent.layout.y;
              }}
              onPress={() => seekTo(line.milliseconds)}
              accessibilityRole="button"
              accessibilityLabel={line.line.length > 0 ? line.line : 'Instrumental'}
              accessibilityHint="Seeks playback to this line"
              style={styles.line}
            >
              <Text
                variant="title"
                tone={index === activeIndex ? 'primary' : 'tertiary'}
                style={index === activeIndex ? styles.activeLine : undefined}
              >
                {line.line.length > 0 ? line.line : '♪'}
              </Text>
            </Pressable>
          ))}
        </ScrollView>
      ) : null}

      {view === 'plain' ? (
        <ScrollView
          testID="lyrics-plain"
          showsVerticalScrollIndicator={false}
          contentContainerStyle={styles.content}
        >
          <Text variant="body" style={styles.plain}>
            {plain.trim()}
          </Text>
        </ScrollView>
      ) : null}

      {view === 'synced' || view === 'plain' ? <Credits query={query} /> : null}
    </View>
  );
}

function Credits({ query }: { query: ReturnType<typeof useLyrics> }): ReactElement | null {
  const writers = query.data?.writers ?? [];
  const copyright = query.data?.copyright ?? '';
  if (writers.length === 0 && copyright.length === 0) return null;
  return (
    <View testID="lyrics-credits" style={styles.credits}>
      {writers.length > 0 ? (
        <Text variant="caption" tone="tertiary">
          Written by {writers.join(', ')}
        </Text>
      ) : null}
      {copyright.length > 0 ? (
        <Text variant="caption" tone="tertiary">
          {copyright}
        </Text>
      ) : null}
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
    paddingBottom: spacing.lg,
    gap: spacing.md,
  },
  headerCenter: { flex: 1, alignItems: 'center' },
  headerSpacer: { width: 44 },
  content: {
    paddingHorizontal: spacing['2xl'],
    paddingBottom: spacing['3xl'] * 2,
  },
  states: { paddingHorizontal: spacing['2xl'], gap: spacing.lg, paddingTop: spacing.xl },
  centered: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  centeredText: { textAlign: 'center' },
  line: { paddingVertical: spacing.sm },
  activeLine: { opacity: 1 },
  plain: { lineHeight: 28 },
  credits: {
    paddingHorizontal: spacing['2xl'],
    paddingVertical: spacing.lg,
    gap: spacing.xs,
  },
});

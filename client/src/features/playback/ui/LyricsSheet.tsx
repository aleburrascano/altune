import { useCallback, useEffect, useRef, type ReactElement } from 'react';
import {
  FlatList,
  ScrollView,
  StyleSheet,
  View,
  type ListRenderItemInfo,
  type PressableProps,
} from 'react-native';
import { useRouter } from 'expo-router';

import { withFeaturing } from '@shared/apiClient/featuredArtists';
import { usePlayback } from '@shared/playback/usePlayback';
import { Text } from '@shared/ui/primitives/Text';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { Skeleton } from '@shared/ui/primitives/Skeleton';
import { useReduceMotion } from '@shared/ui/motion/useMotion';
import { spacing } from '@shared/ui/theme/tokens';

import { useLyrics } from '@features/playback/hooks/useLyrics';
import { activeLineIndex, _lyricsView } from '@features/playback/domain/lyricsSync';
import {
  SheetHeader,
  SheetHeaderCenter,
  SheetHeaderTrailing,
  SheetScreen,
} from '@features/playback/ui/SheetHeader';

const SCROLL_LEAD_PX = 140;

type SyncedLine = NonNullable<ReturnType<typeof useLyrics>['data']>['synced_lines'][number];

export function LyricsSheet(): ReactElement {
  const router = useRouter();
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

  const listRef = useRef<FlatList<SyncedLine> | null>(null);
  useEffect(() => {
    if (activeIndex < 0) return;
    listRef.current?.scrollToIndex({
      index: activeIndex,
      viewOffset: SCROLL_LEAD_PX,
      animated: !reduceMotion,
    });
  }, [activeIndex, reduceMotion]);

  const renderLine = useCallback(
    ({ item, index }: ListRenderItemInfo<SyncedLine>) => (
      <LyricLine line={item} index={index} active={index === activeIndex} onSeek={seekTo} />
    ),
    [activeIndex, seekTo],
  );

  return (
    <SheetScreen testID="lyrics-sheet">
      <SheetHeader onClose={() => router.back()} closeLabel="Close lyrics">
        <SheetHeaderCenter>
          <Text variant="title" numberOfLines={1}>
            {track?.title ?? 'Lyrics'}
          </Text>
          {track ? (
            <Text variant="caption" tone="secondary" numberOfLines={1}>
              {withFeaturing(track.artist, track.featuredArtists)}
            </Text>
          ) : null}
        </SheetHeaderCenter>
        <SheetHeaderTrailing />
      </SheetHeader>

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
        <FlatList
          testID="lyrics-synced"
          ref={listRef}
          data={synced}
          extraData={activeIndex}
          keyExtractor={(line, index) => `${line.milliseconds}-${index}`}
          showsVerticalScrollIndicator={false}
          contentContainerStyle={styles.content}
          onScrollToIndexFailed={(info) =>
            listRef.current?.scrollToOffset({
              offset: Math.max(0, info.averageItemLength * info.index - SCROLL_LEAD_PX),
              animated: !reduceMotion,
            })
          }
          renderItem={renderLine}
        />
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
    </SheetScreen>
  );
}

interface LyricLineProps {
  line: SyncedLine;
  index: number;
  active: boolean;
  onSeek: (ms: number) => void;
}

function lineProps({ line, index }: LyricLineProps): Omit<PressableProps, 'onPress'> {
  return {
    testID: `lyrics-line-${index}`,
    accessibilityRole: 'button',
    accessibilityLabel: line.line.length > 0 ? line.line : 'Instrumental',
    accessibilityHint: 'Seeks playback to this line',
    style: styles.line,
  };
}

function LyricLineText({ line, active }: Pick<LyricLineProps, 'line' | 'active'>): ReactElement {
  const tone = active ? 'primary' : 'tertiary';
  const style = active ? styles.activeLine : undefined;
  return (
    <Text variant="title" tone={tone} style={style}>
      {line.line.length > 0 ? line.line : '♪'}
    </Text>
  );
}

function LyricLine(props: LyricLineProps): ReactElement {
  return (
    <Tappable
      action="playback.lyrics.seek"
      onPress={() => props.onSeek(props.line.milliseconds)}
      {...lineProps(props)}
    >
      <LyricLineText line={props.line} active={props.active} />
    </Tappable>
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

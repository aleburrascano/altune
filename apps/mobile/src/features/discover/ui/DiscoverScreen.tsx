/**
 * DiscoverScreen — Spotify-style sectioned multi-kind search (discover-music-v2).
 *
 * Filter chips (All · Albums · Songs · Artists) sit atop the results. "All" is a
 * blended view: a prominent Top Result card, then capped Albums / Songs / Artists
 * sections with "See all" affordances. A kind chip filters to a flat list of that
 * kind. Confidence is no longer displayed anywhere.
 *
 * TestIDs preserved + extended: discover-search-input, discover-loading,
 * discover-empty-no-query (+ discover-history-row-<i>), discover-results,
 * discover-partial-banner, discover-zero-results, discover-full-error
 * (+ discover-retry), discover-row-<kind>-<position>, discover-filter-<all|album|track|artist>,
 * discover-top-result.
 */

import { useRouter } from 'expo-router';
import type { ReactElement } from 'react';
import { useEffect, useRef, useState } from 'react';
import { FlatList, Pressable, StyleSheet, TextInput, View } from 'react-native';
import { useQueryClient } from '@tanstack/react-query';

import {
  Artwork,
  Button,
  Card,
  Chip,
  Row,
  Screen,
  Skeleton,
  Text,
  fontFamily,
  radius,
  spacing,
  useTheme,
} from '@shared/ui';

import { getSearchState, setSearchState } from '@shared/lib/search-state';

import { DiscoverRow } from './DiscoverRow';
import { useDiscoverSearch } from '../hooks/useDiscoverSearch';
import { useRecordClick } from '../hooks/useRecordClick';
import { useSearchHistory } from '../hooks/useSearchHistory';
import { stashHandoffForDetail } from '../tap';
import {
  SECTION_CAP,
  _cap,
  _groupByKind,
  _sectionOrder,
  _topResult,
  _viewForState,
} from '../state';

import type { SectionKey } from '../state';
import type {
  DiscoveryKind,
  DiscoveryResult,
  SearchHistoryItem,
} from '../../../shared/api-client/discovery';

type ResultsFilter = 'all' | DiscoveryKind;

const FILTER_CHIPS: ReadonlyArray<{ filter: ResultsFilter; label: string; testID: string }> = [
  { filter: 'all', label: 'All', testID: 'discover-filter-all' },
  { filter: 'album', label: 'Albums', testID: 'discover-filter-album' },
  { filter: 'track', label: 'Songs', testID: 'discover-filter-track' },
  { filter: 'artist', label: 'Artists', testID: 'discover-filter-artist' },
];

const SKELETON_ROWS = [0, 1, 2, 3, 4, 5];

export function DiscoverScreen(): ReactElement {
  const theme = useTheme();
  const router = useRouter();
  const savedState = getSearchState();
  const [committedQuery, setCommittedQuery] = useState(savedState.query);
  const [inputValue, setInputValue] = useState(savedState.inputValue);
  const [filter, setFilter] = useState<ResultsFilter>('all');
  const queryClient = useQueryClient();
  const search = useDiscoverSearch(committedQuery);
  const history = useSearchHistory();
  const click = useRecordClick();

  // Persist search state for back-navigation.
  useEffect(() => {
    setSearchState(committedQuery, inputValue);
  }, [committedQuery, inputValue]);

  // Refresh history chips after a search completes (the backend inserts
  // the query into search_history as a side-effect of the search call).
  useEffect(() => {
    if (search.data) {
      void queryClient.invalidateQueries({ queryKey: ['discovery', 'history'] });
    }
  }, [search.data, queryClient]);

  // Reset to the blended "All" view on every newly committed query.
  useEffect(() => {
    setFilter('all');
  }, [committedQuery]);

  const view = _viewForState({
    query: committedQuery,
    isLoading: search.isLoading,
    data: search.data,
    error: search.error as Error | null,
  });

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const DEBOUNCE_MS = 300;
  const MIN_CHARS = 2;

  const onSubmit = (): void => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
    setCommittedQuery(inputValue.trim());
  };
  const onChangeText = (text: string): void => {
    setInputValue(text);
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
    const trimmed = text.trim();
    if (trimmed.length === 0) {
      setCommittedQuery('');
    } else if (trimmed.length >= MIN_CHARS) {
      debounceRef.current = setTimeout(() => {
        setCommittedQuery(trimmed);
      }, DEBOUNCE_MS);
    }
  };
  const onClear = (): void => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
    setInputValue('');
    setCommittedQuery('');
  };
  const onHistoryTap = (item: SearchHistoryItem): void => {
    setInputValue(item.query);
    setCommittedQuery(item.query);
  };
  const onResultTap = (result: DiscoveryResult, position: number): void => {
    // Click tracking stays fire-and-forget (best-effort telemetry, ADR-0007);
    // we do NOT await it before navigating.
    click.mutate({
      query_norm: search.data?.query_norm ?? committedQuery,
      kind: result.kind,
      title: result.title,
      subtitle: result.subtitle ?? null,
      position,
      confidence: result.confidence,
    });
    router.push(stashHandoffForDetail(result));
  };

  let body: ReactElement;
  if (view === 'loading') {
    body = (
      <View testID="discover-loading" style={styles.list}>
        {SKELETON_ROWS.map((i) => (
          <Card key={i} style={styles.skeletonCard}>
            <View style={styles.skeletonRow}>
              <Skeleton width={52} height={52} radius={radius.md} />
              <View style={styles.skeletonText}>
                <Skeleton width="70%" height={14} />
                <Skeleton width="40%" height={12} />
              </View>
            </View>
          </Card>
        ))}
      </View>
    );
  } else if (view === 'full-error') {
    body = (
      <View testID="discover-full-error" style={styles.center}>
        <Text variant="title">Search failed</Text>
        <Text variant="label" tone="secondary" style={styles.centerSub}>
          Something went wrong. Try again.
        </Text>
        <Button
          testID="discover-retry"
          label="Retry"
          onPress={() => setCommittedQuery((q) => (q ? `${q} ` : q).trim() || q)}
        />
      </View>
    );
  } else if (view === 'zero-results') {
    body = (
      <View testID="discover-zero-results" style={styles.center}>
        <Text variant="title">No matches</Text>
        <Text variant="label" tone="secondary" style={styles.centerSub}>
          Try a different search.
        </Text>
      </View>
    );
  } else if (view === 'empty-no-query') {
    const items = history.data?.items ?? [];
    body = (
      <View testID="discover-empty-no-query" style={styles.list}>
        <Text variant="label" tone="tertiary" style={styles.sectionHeader}>
          RECENT SEARCHES
        </Text>
        {items.length === 0 ? (
          <Text variant="body" tone="secondary">
            Search music to get started.
          </Text>
        ) : (
          <View style={styles.chipCloud}>
            {items.map((item, index) => (
              <Chip
                key={item.query_norm}
                testID={`discover-history-row-${index}`}
                label={item.query.length > 40 ? `${item.query.slice(0, 40)}…` : item.query}
                onPress={() => onHistoryTap(item)}
              />
            ))}
          </View>
        )}
      </View>
    );
  } else {
    const results = search.data?.results ?? [];
    body = (
      <View testID="discover-results" style={styles.results}>
        <FilterChips active={filter} onSelect={setFilter} />
        {filter === 'all' ? (
          <BlendedResults
            results={results}
            onResultTap={onResultTap}
            onSeeAll={setFilter}
          />
        ) : (
          <FilteredResults kind={filter} results={results} onResultTap={onResultTap} />
        )}
      </View>
    );
  }

  return (
    <Screen>
      <View style={styles.titleBlock}>
        <Text variant="label" tone="tertiary">
          {_greeting()}
        </Text>
        <Text variant="displayL" style={styles.title}>
          Discover
        </Text>
      </View>
      <View style={styles.header}>
        <View style={styles.inputWrapper}>
          <TextInput
            style={[
              styles.input,
              { backgroundColor: theme.color.surface1, color: theme.color.textPrimary },
            ]}
            placeholder="Search music"
            placeholderTextColor={theme.color.textTertiary}
            value={inputValue}
            onChangeText={onChangeText}
            onSubmitEditing={onSubmit}
            returnKeyType="search"
            testID="discover-search-input"
            accessibilityLabel="Search music"
            autoCapitalize="none"
            autoCorrect={false}
          />
          {inputValue.length > 0 ? (
            <Pressable
              testID="discover-clear-input"
              onPress={onClear}
              accessibilityRole="button"
              accessibilityLabel="Clear search"
              style={({ pressed }) => [styles.clearButton, pressed ? { opacity: 0.5 } : null]}
              hitSlop={8}
            >
              <Text variant="label" tone="secondary" style={styles.clearIcon}>
                ✕
              </Text>
            </Pressable>
          ) : null}
        </View>
      </View>
      {body}
    </Screen>
  );
}

/** Time-of-day greeting above the Discover title. */
function _greeting(): string {
  const hour = new Date().getHours();
  if (hour < 12) {
    return 'Good morning';
  }
  if (hour < 18) {
    return 'Good afternoon';
  }
  return 'Good evening';
}

function FilterChips({
  active,
  onSelect,
}: {
  active: ResultsFilter;
  onSelect: (filter: ResultsFilter) => void;
}): ReactElement {
  return (
    <View style={styles.chipRow}>
      {FILTER_CHIPS.map(({ filter, label, testID }) => (
        <Chip
          key={filter}
          testID={testID}
          label={label}
          selected={active === filter}
          onPress={() => onSelect(filter)}
        />
      ))}
    </View>
  );
}

/** A flat, full list of a single kind (chip != all). */
function FilteredResults({
  kind,
  results,
  onResultTap,
}: {
  kind: DiscoveryKind;
  results: DiscoveryResult[];
  onResultTap: (result: DiscoveryResult, position: number) => void;
}): ReactElement {
  const items = results.filter((r) => r.kind === kind);
  const kindLabel = kind === 'track' ? 'songs' : kind === 'album' ? 'albums' : 'artists';

  if (items.length === 0) {
    return (
      <View testID="discover-filtered-empty" style={styles.filteredEmpty}>
        <Text variant="body" tone="tertiary">
          No {kindLabel} found.
        </Text>
      </View>
    );
  }

  return (
    <FlatList
      data={items}
      keyExtractor={(r) => `${r.kind}-${r.title}-${r.subtitle ?? ''}`}
      renderItem={({ item, index }) => (
        <DiscoverRow result={item} position={index} onPress={onResultTap} />
      )}
      contentContainerStyle={styles.listContent}
      showsVerticalScrollIndicator={false}
    />
  );
}

/** Top Result card + capped Albums / Songs / Artists sections (chip == all). */
function BlendedResults({
  results,
  onResultTap,
  onSeeAll,
}: {
  results: DiscoveryResult[];
  onResultTap: (result: DiscoveryResult, position: number) => void;
  onSeeAll: (filter: ResultsFilter) => void;
}): ReactElement {
  const top = _topResult(results);
  const { albums, songs, artists } = _groupByKind(results);

  const byKind: Record<
    DiscoveryKind,
    { title: string; sectionKey: SectionKey; items: DiscoveryResult[] }
  > = {
    album: { title: 'Albums', sectionKey: 'album', items: albums },
    track: { title: 'Songs', sectionKey: 'song', items: songs },
    artist: { title: 'Artists', sectionKey: 'artist', items: artists },
  };
  // Order containers by which kind best matches the query (the kind whose
  // strongest member ranks earliest), so a song query shows Songs first.
  const order = _sectionOrder(results);
  const sections = (['album', 'track', 'artist'] as const)
    .map((kind) => ({ kind, ...byKind[kind] }))
    .filter((s) => s.items.length > 0)
    .sort((a, b) => order.indexOf(a.sectionKey) - order.indexOf(b.sectionKey));

  return (
    <FlatList
      data={sections}
      keyExtractor={(s) => s.kind}
      ListHeaderComponent={
        top !== null ? <TopResultCard result={top} onPress={onResultTap} /> : null
      }
      renderItem={({ item: section }) => (
        <View style={styles.section}>
          <Text variant="label" tone="tertiary" style={styles.sectionHeader}>
            {section.title.toUpperCase()}
          </Text>
          {_cap(section.items).map((result, index) => (
            <DiscoverRow
              key={`${result.kind}-${result.title}-${result.subtitle ?? ''}`}
              result={result}
              position={index}
              onPress={onResultTap}
            />
          ))}
          {section.items.length > SECTION_CAP ? (
            <Pressable
              testID={`discover-see-all-${section.kind}`}
              onPress={() => onSeeAll(section.kind)}
              accessibilityRole="button"
              accessibilityLabel={`See all ${section.title.toLowerCase()}`}
              style={({ pressed }) => [styles.seeAll, pressed ? { opacity: 0.7 } : null]}
            >
              <Text variant="label" tone="accent">
                See all {section.title.toLowerCase()}
              </Text>
            </Pressable>
          ) : null}
        </View>
      )}
      contentContainerStyle={styles.listContent}
      showsVerticalScrollIndicator={false}
    />
  );
}

/** Larger, prominent card for the single highest-ranked result. */
function TopResultCard({
  result,
  onPress,
}: {
  result: DiscoveryResult;
  onPress: (result: DiscoveryResult, position: number) => void;
}): ReactElement {
  const isArtist = result.kind === 'artist';
  const kindLabel = isArtist ? 'Artist' : result.kind === 'album' ? 'Album' : 'Song';
  return (
    <View style={styles.topResultWrap}>
      <Text variant="label" tone="tertiary" style={styles.sectionHeader}>
        TOP RESULT
      </Text>
      <Pressable
        testID="discover-top-result"
        onPress={() => onPress(result, 0)}
        accessibilityRole="button"
        accessibilityLabel={`${result.title}${result.subtitle ? `, ${result.subtitle}` : ''}, ${kindLabel}`}
        style={({ pressed }) => (pressed ? { opacity: 0.85 } : null)}
      >
        <Card>
          <Row
            leading={
              <Artwork
                uri={result.image_url}
                size={88}
                radius={isArtist ? radius.full : radius.lg}
                accessibilityLabel={result.title}
              />
            }
          >
            <Text variant="title" numberOfLines={2}>
              {result.title}
            </Text>
            {result.subtitle !== null ? (
              <Text variant="body" tone="secondary" numberOfLines={1} style={{ marginTop: 2 }}>
                {result.subtitle}
              </Text>
            ) : null}
            <Text variant="caption" tone="tertiary" style={{ marginTop: spacing.sm }}>
              {kindLabel}
            </Text>
          </Row>
        </Card>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  titleBlock: { paddingTop: spacing.sm },
  title: { marginTop: spacing.xs },
  header: { paddingTop: spacing.md, paddingBottom: spacing.md },
  inputWrapper: { position: 'relative' as const, justifyContent: 'center' as const },
  input: {
    borderRadius: radius.md,
    paddingHorizontal: spacing.lg,
    paddingRight: 44,
    paddingVertical: spacing.md,
    fontFamily: fontFamily.bodyRegular,
    fontSize: 16,
  },
  clearButton: {
    position: 'absolute' as const,
    right: spacing.md,
    width: 28,
    height: 28,
    borderRadius: 14,
    alignItems: 'center' as const,
    justifyContent: 'center' as const,
  },
  clearIcon: { fontSize: 14 },
  list: { flex: 1, paddingTop: spacing.sm },
  results: { flex: 1 },
  listContent: { paddingTop: spacing.sm, paddingBottom: spacing.xl },
  skeletonCard: { marginBottom: spacing.sm },
  skeletonRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.md },
  skeletonText: { flex: 1, gap: spacing.sm },
  sectionHeader: { marginBottom: spacing.md, letterSpacing: 1 },
  chipRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    paddingBottom: spacing.md,
    flexWrap: 'wrap',
  },
  chipCloud: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  section: { marginBottom: spacing.lg },
  topResultWrap: { marginBottom: spacing.lg },
  seeAll: { paddingVertical: spacing.md, alignSelf: 'flex-start', minHeight: 44 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  centerSub: { marginTop: spacing.xs, marginBottom: spacing.lg },
  filteredEmpty: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
});

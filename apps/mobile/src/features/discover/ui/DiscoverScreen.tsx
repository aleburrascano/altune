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

import type { ReactElement } from 'react';
import { useEffect, useState } from 'react';
import { FlatList, Pressable, StyleSheet, TextInput, View } from 'react-native';

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

import { DiscoverRow } from './DiscoverRow';
import { useDiscoverSearch } from '../hooks/useDiscoverSearch';
import { useRecordClick } from '../hooks/useRecordClick';
import { useSearchHistory } from '../hooks/useSearchHistory';
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
  const [committedQuery, setCommittedQuery] = useState('');
  const [inputValue, setInputValue] = useState('');
  const [filter, setFilter] = useState<ResultsFilter>('all');
  const search = useDiscoverSearch(committedQuery);
  const history = useSearchHistory();
  const click = useRecordClick();

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

  const onSubmit = (): void => setCommittedQuery(inputValue.trim());
  const onHistoryTap = (item: SearchHistoryItem): void => {
    setInputValue(item.query);
    setCommittedQuery(item.query);
  };
  const onResultTap = (result: DiscoveryResult, position: number): void => {
    click.mutate({
      query_norm: search.data?.query_norm ?? committedQuery,
      kind: result.kind,
      title: result.title,
      subtitle: result.subtitle ?? null,
      position,
      confidence: result.confidence,
    });
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
        <TextInput
          style={[
            styles.input,
            { backgroundColor: theme.color.surface1, color: theme.color.textPrimary },
          ]}
          placeholder="Search music"
          placeholderTextColor={theme.color.textTertiary}
          value={inputValue}
          onChangeText={setInputValue}
          onSubmitEditing={onSubmit}
          returnKeyType="search"
          testID="discover-search-input"
          autoCapitalize="none"
          autoCorrect={false}
        />
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
  input: {
    borderRadius: radius.md,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    fontFamily: fontFamily.bodyRegular,
    fontSize: 16,
  },
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
  seeAll: { paddingVertical: spacing.sm, alignSelf: 'flex-start' },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  centerSub: { marginTop: spacing.xs, marginBottom: spacing.lg },
});

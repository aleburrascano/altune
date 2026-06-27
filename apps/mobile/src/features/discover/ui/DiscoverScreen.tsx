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
import { Keyboard, Pressable, StyleSheet, View } from 'react-native';

import { Screen, SearchBar, Text, spacing, useTheme } from '@shared/ui';
import { DiscoverBody } from './DiscoverBody';
import { SuggestionsList } from './SuggestionsList';
import { useDiscoverLogic } from '../hooks/useDiscoverLogic';

export function DiscoverScreen(): ReactElement {
  const theme = useTheme();
  const d = useDiscoverLogic();

  return (
    <Screen>
      <Pressable onPress={Keyboard.dismiss} style={styles.flex}>
      <View style={styles.titleBlock}>
        <Text variant="displayL" style={styles.title}>
          Discover
        </Text>
      </View>
      <SearchBar
        value={d.inputValue}
        onChangeText={d.onChangeText}
        onSubmitEditing={d.onSubmit}
        onClear={d.onClear}
        onFocus={() => d.setIsFocused(true)}
        onBlur={() => d.setIsFocused(false)}
        focused={d.isFocused}
        pending={d.pending}
        suggestionsOpen={d.showSuggestions}
        placeholder="Search music"
        testID="discover-search-input"
        theme={theme}
      >
        {d.showSuggestions && (
          <SuggestionsList
            suggestions={d.suggestionItems}
            onSelect={d.onSuggestionSelect}
          />
        )}
      </SearchBar>
      <DiscoverBody
        view={d.view}
        searchData={d.searchData}
        history={d.history}
        filter={d.filter}
        onFilterChange={d.setFilter}
        onHistoryTap={d.onHistoryTap}
        onResultTap={d.onResultTap}
        impression={d.impression}
        onRetry={d.onRetry}
        onRefresh={d.onRefresh}
        isRefreshing={d.isRefreshing}
        correctedQuery={d.correctedQuery}
        originalQuery={d.originalQuery}
        onSearchOriginal={d.onSearchOriginal}
        onClearHistory={d.onClearHistory}
      />
      </Pressable>
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  titleBlock: { paddingTop: spacing.sm },
  title: { marginTop: spacing.xs },
});

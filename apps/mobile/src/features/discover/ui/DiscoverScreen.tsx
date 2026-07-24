import type { ReactElement } from 'react';
import { Keyboard, Pressable, StyleSheet, View } from 'react-native';

import { Screen, Text, spacing, useTheme } from '@shared/ui';
import { SearchBar } from '@shared/ui/primitives/SearchBar';
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
            <SuggestionsList suggestions={d.suggestionItems} onSelect={d.onSuggestionSelect} />
          )}
        </SearchBar>
        <DiscoverBody
          view={d.view}
          searchData={d.searchData}
          historyItems={d.historyItems}
          filter={d.filter}
          onFilterChange={d.setFilter}
          onHistoryTap={d.onHistoryTap}
          onResultTap={d.onResultTap}
          impression={d.impression}
          onRetry={d.onRetry}
          searchError={d.searchError}
          onEndReached={d.onEndReached}
          isFetchingNextPage={d.isFetchingNextPage}
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

import { useCallback, useRef, type ReactElement, type ReactNode } from 'react';
import { useFocusEffect } from 'expo-router';
import { Keyboard, Platform, Pressable, StyleSheet, View, type TextInput } from 'react-native';

import { Screen, Text, spacing, useTheme } from '@shared/ui';
import { SearchBar } from '@shared/ui/primitives/SearchBar';
import { registerSearchFocus } from '@shared/ui/keyboard/useKeyboardShortcuts';
import { DiscoverBody } from './DiscoverBody';
import { SuggestionsList } from './SuggestionsList';
import { useDiscoverLogic } from '../hooks/useDiscoverLogic';
import { MAX_QUERY_LENGTH } from '../searchLimits';

type AreaProps = { children: ReactNode };

function WebScreenBody({ children }: AreaProps): ReactElement {
  return (
    <View testID="discover-screen-body" style={styles.flex}>
      {children}
    </View>
  );
}

function NativeScreenBody({ children }: AreaProps): ReactElement {
  return (
    <Pressable testID="discover-screen-body" onPress={Keyboard.dismiss} style={styles.flex}>
      {children}
    </Pressable>
  );
}

function DismissKeyboardArea({ children }: AreaProps): ReactElement {
  const Body = Platform.OS === 'web' ? WebScreenBody : NativeScreenBody;
  return <Body>{children}</Body>;
}

export function DiscoverScreen(): ReactElement {
  const theme = useTheme();
  const d = useDiscoverLogic();
  const searchInputRef = useRef<TextInput>(null);

  useFocusEffect(
    useCallback(() => registerSearchFocus(() => searchInputRef.current?.focus()), []),
  );

  return (
    <Screen>
      <DismissKeyboardArea>
        <View style={styles.titleBlock}>
          <Text variant="displayL" style={styles.title}>
            Discover
          </Text>
        </View>
        <SearchBar
          ref={searchInputRef}
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
          maxLength={MAX_QUERY_LENGTH}
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
          resultsIncomplete={d.resultsIncomplete}
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
          correction={d.correction}
          onSearchOriginal={d.onSearchOriginal}
          onClearHistory={d.onClearHistory}
          nextPageFailed={d.nextPageFailed}
          onRetryNextPage={d.onRetryNextPage}
          clearHistoryFailed={d.clearHistoryFailed}
          refreshFailed={d.refreshFailed}
        />
      </DismissKeyboardArea>
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  titleBlock: { paddingTop: spacing.sm },
  title: { marginTop: spacing.xs },
});

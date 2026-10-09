import { useCallback, useRef, type ReactElement } from 'react';
import { useFocusEffect } from 'expo-router';
import { StyleSheet, View, type TextInput } from 'react-native';

import { Screen, Text, spacing, useTheme } from '@shared/ui';
import { SearchBar } from '@shared/ui/primitives/SearchBar';
import { DismissKeyboardArea } from '@shared/ui/keyboard/keyboard';
import { registerSearchFocus } from '@shared/ui/keyboard/useKeyboardShortcuts';
import { DiscoverBody } from '@features/discovery/ui/DiscoverBody';
import { SuggestionsList } from '@features/discovery/ui/SuggestionsList';
import { useDiscoverLogic } from '@features/discovery/hooks/useDiscoverLogic';
import { MAX_QUERY_LENGTH } from '@features/discovery/domain/searchLimits';

export function DiscoverScreen(): ReactElement {
  const theme = useTheme();
  const d = useDiscoverLogic();
  const searchInputRef = useRef<TextInput>(null);

  useFocusEffect(useCallback(() => registerSearchFocus(() => searchInputRef.current?.focus()), []));

  return (
    <Screen>
      <DismissKeyboardArea testID="discover-screen-body" style={styles.flex}>
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
        <DiscoverBody results={d.results} pagination={d.pagination} history={d.history} />
      </DismissKeyboardArea>
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  titleBlock: { paddingTop: spacing.sm },
  title: { marginTop: spacing.xs },
});

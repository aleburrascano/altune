import { useCallback, useRef, type ReactElement } from 'react';
import { StyleSheet, View, type TextInput } from 'react-native';

import {
  Screen,
  Text,
  spacing,
  useTheme,
  SearchBar,
  DismissKeyboardArea,
  registerSearchFocus,
} from '@shared/ui';
import type { DiscoverEnv } from '@features/discovery/view/search/screenEnv';
import { DiscoverBody } from '@features/discovery/view/search/DiscoverBody';
import { SuggestionsList } from '@features/discovery/view/history/SuggestionsList';
import { useDiscoverLogic } from '@features/discovery/view/search/useDiscoverLogic';
import { MAX_QUERY_LENGTH } from '@features/discovery/core/searchPaging';

export function DiscoverScreen(env: DiscoverEnv): ReactElement {
  const theme = useTheme();
  const d = useDiscoverLogic(env);
  const searchInputRef = useRef<TextInput>(null);

  env.useFocus(useCallback(() => registerSearchFocus(() => searchInputRef.current?.focus()), []));

  return (
    <Screen>
      <DismissKeyboardArea
        testID="discover-screen-body"
        onDismiss={env.dismissKeyboard}
        style={styles.flex}
      >
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

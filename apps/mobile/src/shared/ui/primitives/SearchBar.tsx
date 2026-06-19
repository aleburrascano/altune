import type { ReactElement, ReactNode } from 'react';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';
import { Search, X } from 'lucide-react-native';

import { fontFamily, radius, spacing, typography } from '../theme/tokens';

interface SearchBarTheme {
  color: {
    surface1: string;
    textPrimary: string;
    textTertiary: string;
    accent: string;
  };
}

export interface SearchBarProps {
  value: string;
  onChangeText: (text: string) => void;
  onSubmitEditing: () => void;
  onClear: () => void;
  onFocus?: () => void;
  onBlur?: () => void;
  focused?: boolean;
  pending?: boolean;
  suggestionsOpen?: boolean;
  placeholder?: string;
  testID?: string;
  children?: ReactNode;
  theme: SearchBarTheme;
}

export function SearchBar({
  value,
  onChangeText,
  onSubmitEditing,
  onClear,
  onFocus,
  onBlur,
  focused = false,
  pending = false,
  suggestionsOpen = false,
  placeholder = 'Search music',
  testID = 'search-input',
  children,
  theme,
}: SearchBarProps): ReactElement {
  return (
    <View style={styles.wrapper}>
      <View style={styles.inputAnchor}>
        <View style={styles.inputWrapper}>
          <Search size={16} color={theme.color.textTertiary} style={styles.searchIcon} />
          <TextInput
            style={[
              styles.input,
              { backgroundColor: theme.color.surface1, color: theme.color.textPrimary },
              { borderWidth: 1, borderColor: focused ? theme.color.accent : 'transparent' },
              suggestionsOpen ? { borderBottomLeftRadius: 0, borderBottomRightRadius: 0 } : null,
            ]}
            placeholder={placeholder}
            placeholderTextColor={theme.color.textTertiary}
            value={value}
            onChangeText={onChangeText}
            onSubmitEditing={onSubmitEditing}
            onFocus={onFocus}
            onBlur={onBlur}
            returnKeyType="search"
            testID={testID}
            accessibilityLabel={placeholder}
            autoCapitalize="none"
            autoCorrect={false}
          />
          {value.length > 0 ? (
            <Pressable
              testID={`${testID}-clear`}
              onPress={onClear}
              accessibilityRole="button"
              accessibilityLabel="Clear search"
              style={({ pressed }) => [styles.clearButton, pressed ? { opacity: 0.5 } : null]}
              hitSlop={8}
            >
              <X size={16} color={theme.color.textTertiary} />
            </Pressable>
          ) : null}
        </View>
        {children}
      </View>
      {pending ? (
        <View style={[styles.pendingBar, { backgroundColor: theme.color.accent }]} />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  wrapper: { paddingTop: spacing.md, paddingBottom: spacing.md, zIndex: 10 },
  inputAnchor: { position: 'relative', zIndex: 10 },
  inputWrapper: { position: 'relative', justifyContent: 'center' },
  searchIcon: {
    position: 'absolute',
    left: spacing.lg,
    zIndex: 1,
  },
  input: {
    borderRadius: radius.md,
    paddingLeft: 44,
    paddingRight: 44,
    paddingVertical: spacing.md,
    fontFamily: fontFamily.bodyRegular,
    fontSize: typography.body.fontSize,
  },
  clearButton: {
    position: 'absolute',
    right: spacing.md,
    width: 32,
    height: 32,
    borderRadius: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },
  pendingBar: {
    height: 2,
    borderRadius: 1,
    marginTop: spacing.xs,
    opacity: 0.6,
  },
});

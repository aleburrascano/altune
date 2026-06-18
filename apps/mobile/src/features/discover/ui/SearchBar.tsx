import type { ReactElement } from 'react';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';
import { X } from 'lucide-react-native';

import { fontFamily, radius, spacing, typography } from '@shared/ui';

interface SearchBarTheme {
  color: {
    surface1: string;
    textPrimary: string;
    textTertiary: string;
  };
}

interface SearchBarProps {
  value: string;
  onChangeText: (text: string) => void;
  onSubmitEditing: () => void;
  onClear: () => void;
  onFocus?: () => void;
  onBlur?: () => void;
  theme: SearchBarTheme;
}

export function SearchBar({
  value,
  onChangeText,
  onSubmitEditing,
  onClear,
  onFocus,
  onBlur,
  theme,
}: SearchBarProps): ReactElement {
  return (
    <View style={styles.header}>
      <View style={styles.inputWrapper}>
        <TextInput
          style={[
            styles.input,
            { backgroundColor: theme.color.surface1, color: theme.color.textPrimary },
          ]}
          placeholder="Search music"
          placeholderTextColor={theme.color.textTertiary}
          value={value}
          onChangeText={onChangeText}
          onSubmitEditing={onSubmitEditing}
          onFocus={onFocus}
          onBlur={onBlur}
          returnKeyType="search"
          testID="discover-search-input"
          accessibilityLabel="Search music"
          autoCapitalize="none"
          autoCorrect={false}
        />
        {value.length > 0 ? (
          <Pressable
            testID="discover-clear-input"
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
    </View>
  );
}

const styles = StyleSheet.create({
  header: { paddingTop: spacing.md, paddingBottom: spacing.md },
  inputWrapper: { position: 'relative', justifyContent: 'center' },
  input: {
    borderRadius: radius.md,
    paddingHorizontal: spacing.lg,
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
});

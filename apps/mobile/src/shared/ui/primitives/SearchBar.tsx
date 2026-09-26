import { forwardRef, type ForwardedRef, type ReactElement, type ReactNode } from 'react';
import { Pressable, StyleSheet, TextInput, View, type TextInputProps } from 'react-native';
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
  maxLength?: number;
  testID?: string;
  children?: ReactNode;
  theme: SearchBarTheme;
}

function inputStyle(theme: SearchBarTheme, focused: boolean, suggestionsOpen: boolean): TextInputProps['style'] {
  return [
    styles.input,
    { backgroundColor: theme.color.surface1, color: theme.color.textPrimary },
    { borderWidth: 1, borderColor: focused ? theme.color.accent : 'transparent' },
    suggestionsOpen ? { borderBottomLeftRadius: 0, borderBottomRightRadius: 0 } : null,
  ];
}

function fieldProps(props: SearchBarProps): TextInputProps {
  const placeholder = props.placeholder ?? 'Search music';
  const testID = props.testID ?? 'search-input';
  const shared = { value: props.value, onChangeText: props.onChangeText, testID, placeholder };
  const editing = { onSubmitEditing: props.onSubmitEditing, onFocus: props.onFocus, onBlur: props.onBlur };
  const fixed = { returnKeyType: 'search' as const, autoCapitalize: 'none' as const, autoCorrect: false };
  return { ...shared, ...editing, ...fixed, maxLength: props.maxLength, placeholderTextColor: props.theme.color.textTertiary, accessibilityLabel: placeholder };
}

interface ClearButtonProps {
  testID: string;
  tertiary: string;
  onClear: () => void;
}

function ClearButton({ testID, tertiary, onClear }: ClearButtonProps): ReactElement {
  return (
    <Pressable testID={`${testID}-clear`} onPress={onClear} {...CLEAR_BUTTON_PROPS}>
      <X size={16} color={tertiary} />
    </Pressable>
  );
}

interface SearchFieldProps {
  props: SearchBarProps;
  ref: ForwardedRef<TextInput>;
}

function SearchField({ props, ref }: SearchFieldProps): ReactElement {
  const testID = props.testID ?? 'search-input';
  return (
    <View style={styles.inputWrapper}>
      <Search size={16} color={props.theme.color.textTertiary} style={styles.searchIcon} />
      <TextInput ref={ref} style={inputStyle(props.theme, !!props.focused, !!props.suggestionsOpen)} {...fieldProps(props)} />
      {props.value.length > 0 ? <ClearButton testID={testID} tertiary={props.theme.color.textTertiary} onClear={props.onClear} /> : null}
    </View>
  );
}

export const SearchBar = forwardRef(searchBarWithRef);

function searchBarWithRef(props: SearchBarProps, ref: ForwardedRef<TextInput>): ReactElement {
  return (
    <View style={styles.wrapper}>
      <View style={styles.inputAnchor}>
        <SearchField props={props} ref={ref} />{props.children}
      </View>
      {props.pending ? <View style={[styles.pendingBar, { backgroundColor: props.theme.color.accent }]} /> : null}
    </View>
  );
}

const CLEAR_BUTTON_PROPS = {
  accessibilityRole: 'button' as const,
  accessibilityLabel: 'Clear search',
  hitSlop: 8,
  style: ({ pressed }: { pressed: boolean }) => [styles.clearButton, pressed ? { opacity: 0.5 } : null],
};

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

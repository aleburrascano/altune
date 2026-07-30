import { Eye, EyeOff } from 'lucide-react-native';
import { useState, type ReactElement } from 'react';
import { Pressable, StyleSheet, TextInput, View, type TextInputProps } from 'react-native';

import { fontFamily, minInteractiveHeight, radius, spacing, typography } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

export interface TextFieldProps extends Omit<
  TextInputProps,
  'style' | 'placeholderTextColor' | 'secureTextEntry'
> {
  secure?: boolean;
  surface?: 'surface1' | 'surface2';
  error?: boolean;
  rows?: number;
}

const rowHeight = typography.body.lineHeight;

export function TextField({
  secure = false,
  surface = 'surface1',
  error = false,
  rows,
  onFocus,
  onBlur,
  placeholder,
  accessibilityLabel,
  testID,
  ...rest
}: TextFieldProps): ReactElement {
  const theme = useTheme();
  const [focused, setFocused] = useState(false);
  const [revealed, setRevealed] = useState(false);

  return (
    <View style={styles.wrapper}>
      <TextInput
        {...rest}
        testID={testID}
        placeholder={placeholder}
        placeholderTextColor={theme.color.textTertiary}
        accessibilityLabel={accessibilityLabel ?? placeholder}
        secureTextEntry={secure && !revealed}
        onFocus={(e) => {
          setFocused(true);
          onFocus?.(e);
        }}
        onBlur={(e) => {
          setFocused(false);
          onBlur?.(e);
        }}
        style={[
          styles.input,
          {
            color: theme.color.textPrimary,
            backgroundColor: theme.color[surface],
            borderColor: error
              ? theme.color.danger
              : focused
                ? theme.color.accent
                : theme.color.border,
          },
          secure ? styles.inputSecure : null,
          rows != null
            ? { minHeight: rows * rowHeight + spacing.lg, textAlignVertical: 'top' as const }
            : null,
        ]}
      />
      {secure ? (
        <Pressable
          testID={testID ? `${testID}-reveal` : undefined}
          onPress={() => setRevealed((v) => !v)}
          accessibilityRole="button"
          accessibilityLabel={revealed ? 'Hide password' : 'Show password'}
          hitSlop={8}
          style={({ pressed }) => [styles.reveal, pressed ? { opacity: 0.5 } : null]}
        >
          {revealed ? (
            <EyeOff size={18} color={theme.color.textTertiary} />
          ) : (
            <Eye size={18} color={theme.color.textTertiary} />
          )}
        </Pressable>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  wrapper: { position: 'relative', justifyContent: 'center' },
  input: {
    minHeight: minInteractiveHeight,
    borderWidth: 1,
    borderRadius: radius.md,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    fontFamily: fontFamily.bodyRegular,
    fontSize: typography.body.fontSize,
  },
  inputSecure: { paddingRight: 44 },
  reveal: {
    position: 'absolute',
    right: spacing.xs,
    width: 44,
    height: 44,
    alignItems: 'center',
    justifyContent: 'center',
  },
});

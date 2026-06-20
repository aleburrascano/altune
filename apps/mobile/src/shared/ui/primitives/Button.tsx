import * as Haptics from 'expo-haptics';
import { ActivityIndicator, Animated, Pressable, StyleSheet } from 'react-native';
import type { StyleProp, ViewStyle } from 'react-native';

import { usePressScale } from '../motion/pressScale';
import type { Theme } from '../theme/theme';
import { minInteractiveHeight, radius, spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';
import { Text } from './Text';
import type { TextTone } from './Text';

export type ButtonVariant = 'primary' | 'secondary' | 'ghost';

export type ButtonProps = {
  label: string;
  onPress: () => void;
  variant?: ButtonVariant;
  disabled?: boolean;
  loading?: boolean;
  /** Fire a light haptic on press (key actions only). */
  haptic?: boolean;
  testID?: string;
  style?: StyleProp<ViewStyle>;
};

function backgroundFor(theme: Theme, variant: ButtonVariant, pressed: boolean): string {
  if (variant === 'primary') {
    return pressed ? theme.color.accentPressed : theme.color.accent;
  }
  if (variant === 'secondary') {
    return pressed ? theme.color.surface2 : theme.color.surface1;
  }
  return 'transparent';
}

function labelTone(variant: ButtonVariant): TextTone {
  if (variant === 'primary') {
    return 'onAccent';
  }
  if (variant === 'ghost') {
    return 'accent';
  }
  return 'primary';
}

export function Button({
  label,
  onPress,
  variant = 'primary',
  disabled = false,
  loading = false,
  haptic = false,
  testID,
  style,
}: ButtonProps) {
  const theme = useTheme();
  const { onPressIn, onPressOut, animatedStyle } = usePressScale();
  const isDisabled = disabled || loading;

  const handlePress = () => {
    if (isDisabled) {
      return;
    }
    if (haptic) {
      void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    }
    onPress();
  };

  return (
    <Animated.View style={animatedStyle}>
      <Pressable
        testID={testID}
        onPress={handlePress}
        onPressIn={onPressIn}
        onPressOut={onPressOut}
        disabled={isDisabled}
        accessibilityRole="button"
        accessibilityState={{ disabled: isDisabled, busy: loading }}
        style={({ pressed }) => [
          styles.base,
          {
            backgroundColor: backgroundFor(theme, variant, pressed),
            opacity: isDisabled ? 0.5 : 1,
          },
          style,
        ]}
      >
        {loading ? (
          <ActivityIndicator
            color={variant === 'primary' ? theme.color.onAccent : theme.color.accent}
          />
        ) : (
          <Text variant="bodyStrong" tone={labelTone(variant)}>
            {label}
          </Text>
        )}
      </Pressable>
    </Animated.View>
  );
}

const styles = StyleSheet.create({
  base: {
    minHeight: minInteractiveHeight,
    borderRadius: radius.lg,
    paddingHorizontal: spacing.xl,
    alignItems: 'center',
    justifyContent: 'center',
    flexDirection: 'row',
  },
});

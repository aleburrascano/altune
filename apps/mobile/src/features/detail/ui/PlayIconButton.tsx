import type { ReactElement } from 'react';
import { Pressable, StyleSheet } from 'react-native';

import { Pause, Play } from 'lucide-react-native';

import { useTheme } from '@shared/ui/theme';

export function PlayIconButton({
  testID,
  isPlaying,
  disabled,
  onPress,
}: {
  testID: string;
  isPlaying: boolean;
  disabled: boolean;
  onPress: () => void;
}): ReactElement {
  const theme = useTheme();
  return (
    <Pressable
      testID={testID}
      onPress={onPress}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityLabel={isPlaying ? 'Pause' : 'Play'}
      style={({ pressed }) => [
        styles.btn,
        { backgroundColor: theme.color.accent },
        disabled ? styles.disabled : null,
        pressed && !disabled ? styles.pressed : null,
      ]}
    >
      {isPlaying ? (
        <Pause size={24} color={theme.color.onAccent} />
      ) : (
        <Play size={24} color={theme.color.onAccent} />
      )}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  btn: {
    width: 56,
    height: 56,
    borderRadius: 28,
    alignItems: 'center',
    justifyContent: 'center',
  },
  disabled: { opacity: 0.5 },
  pressed: { opacity: 0.8 },
});

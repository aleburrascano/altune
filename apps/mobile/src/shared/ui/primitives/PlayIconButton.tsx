import type { ReactElement } from 'react';
import { Pressable, StyleSheet } from 'react-native';

import { Pause, Play } from 'lucide-react-native';

import { useTheme } from '@shared/ui/theme';

type PlayIconButtonProps = {
  testID?: string;
  isPlaying: boolean;
  disabled?: boolean;
  onPress: () => void;
  size?: number;
};

export function PlayIconButton({
  testID,
  isPlaying,
  disabled = false,
  onPress,
  size = 56,
}: PlayIconButtonProps): ReactElement {
  const theme = useTheme();
  const iconSize = Math.round(size * 0.43);
  return (
    <Pressable
      testID={testID}
      onPress={onPress}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityLabel={isPlaying ? 'Pause' : 'Play'}
      style={({ pressed }) => [
        styles.btn,
        { width: size, height: size, borderRadius: size / 2, backgroundColor: theme.color.accent },
        disabled ? styles.disabled : null,
        pressed && !disabled ? styles.pressed : null,
      ]}
    >
      {isPlaying ? (
        <Pause size={iconSize} color={theme.color.onAccent} />
      ) : (
        <Play size={iconSize} color={theme.color.onAccent} />
      )}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  btn: {
    alignItems: 'center',
    justifyContent: 'center',
  },
  disabled: { opacity: 0.5 },
  pressed: { opacity: 0.8 },
});

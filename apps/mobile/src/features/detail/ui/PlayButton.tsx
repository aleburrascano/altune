/**
 * PlayButton — the round primary play control shared by the track, album, and
 * artist detail heroes.
 *
 * A 50pt cobalt circle with a triangle (play) / bars (pause) glyph, a soft
 * accent glow, and a light haptic. Disabled state (nothing to play) drops to a
 * muted surface fill. Feature-local: all three consumers live in `detail/`, so
 * it stays here rather than in shared/ui (promotion needs 2+ *features*).
 */

import type { ReactElement } from 'react';
import * as Haptics from 'expo-haptics';
import { Pressable, StyleSheet, View } from 'react-native';

import { Pause, Play } from 'lucide-react-native';

import { useTheme } from '@shared/ui/theme';
import { radius } from '@shared/ui/theme/tokens';

const SIZE = 50;

export function PlayButton({
  playing,
  disabled = false,
  onPress,
  testID,
  accessibilityLabel,
}: {
  playing: boolean;
  disabled?: boolean;
  onPress: () => void;
  testID?: string;
  accessibilityLabel: string;
}): ReactElement {
  const theme = useTheme();

  const handlePress = (): void => {
    if (disabled) {
      return;
    }
    void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    onPress();
  };

  return (
    <View style={styles.wrap}>
      <Pressable
        testID={testID}
        onPress={handlePress}
        disabled={disabled}
        accessibilityRole="button"
        accessibilityLabel={accessibilityLabel}
        accessibilityState={{ disabled }}
        hitSlop={8}
        style={({ pressed }) => [
          styles.circle,
          {
            backgroundColor: disabled
              ? theme.color.surface2
              : pressed
                ? theme.color.accentPressed
                : theme.color.accent,
            boxShadow: disabled ? 'none' : `0px 8px 16px ${theme.color.accent}73`,
          },
        ]}
      >
        {playing ? (
          <Pause size={22} color={theme.color.onAccent} fill={theme.color.onAccent} />
        ) : (
          <Play
            size={22}
            color={disabled ? theme.color.textTertiary : theme.color.onAccent}
            fill={disabled ? theme.color.textTertiary : theme.color.onAccent}
            style={styles.playGlyph}
          />
        )}
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { alignItems: 'center' },
  circle: {
    width: SIZE,
    height: SIZE,
    borderRadius: radius.full,
    alignItems: 'center',
    justifyContent: 'center',
  },
  // optical-center the triangle (its visual mass sits left of geometric center)
  playGlyph: { marginLeft: 3 },
});

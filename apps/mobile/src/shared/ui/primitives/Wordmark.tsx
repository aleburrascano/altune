import { Text as RNText, View } from 'react-native';

import { fontFamily } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

export type WordmarkProps = {
  size?: number;
};

/** The Altune wordmark: lowercase "altune" in Plus Jakarta Sans + a cobalt dot. */
export function Wordmark({ size = 28 }: WordmarkProps) {
  const theme = useTheme();
  const dot = Math.max(4, Math.round(size * 0.13));
  return (
    <View
      accessibilityRole="header"
      accessibilityLabel="altune"
      style={{ flexDirection: 'row', alignItems: 'flex-end' }}
    >
      <RNText
        style={{
          fontFamily: fontFamily.displaySemiBold,
          fontSize: size,
          color: theme.color.textPrimary,
        }}
      >
        altune
      </RNText>
      <View
        style={{
          width: dot,
          height: dot,
          borderRadius: dot / 2,
          backgroundColor: theme.color.accent,
          marginLeft: 3,
          // Lift the dot to sit on the type baseline (flex-end aligns it to the
          // text box bottom, which sits below the glyphs' visual bottom).
          marginBottom: size * 0.2,
        }}
      />
    </View>
  );
}

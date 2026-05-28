import { View } from 'react-native';
import type { ViewProps } from 'react-native';

import { glowStyle, radius, spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

export type CardProps = ViewProps & {
  /** Adds the soft indigo glow used for the active/selected state. */
  active?: boolean;
  surface?: 'surface1' | 'surface2';
};

export function Card({ active = false, surface = 'surface1', style, ...rest }: CardProps) {
  const theme = useTheme();
  return (
    <View
      style={[
        { backgroundColor: theme.color[surface], borderRadius: radius.lg, padding: spacing.lg },
        active ? glowStyle(theme.color.accentGlow) : null,
        style,
      ]}
      {...rest}
    />
  );
}

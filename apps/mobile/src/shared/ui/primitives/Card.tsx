import { View } from 'react-native';
import type { ViewProps } from 'react-native';

import { radius, spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

export type CardProps = ViewProps & {
  /** Adds a 1px accent border for the active/selected state. */
  active?: boolean;
  surface?: 'surface1' | 'surface2';
};

export function Card({ active = false, surface = 'surface1', style, ...rest }: CardProps) {
  const theme = useTheme();
  return (
    <View
      style={[
        { backgroundColor: theme.color[surface], borderRadius: radius.lg, padding: spacing.lg },
        active ? { borderWidth: 1, borderColor: theme.color.accent } : null,
        style,
      ]}
      {...rest}
    />
  );
}

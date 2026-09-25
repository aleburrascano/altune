import { View } from 'react-native';
import type { ViewProps } from 'react-native';

import { radius, spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

export type CardProps = ViewProps & {
  surface?: 'surface2';
};

export function Card({ surface = 'surface2', style, ...rest }: CardProps) {
  const theme = useTheme();
  return (
    <View
      style={[
        { backgroundColor: theme.color[surface], borderRadius: radius.lg, padding: spacing.lg },
        style,
      ]}
      {...rest}
    />
  );
}

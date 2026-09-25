import { View } from 'react-native';
import type { ViewProps } from 'react-native';

import { radius, spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

export type CardProps = ViewProps;

function useCardStyle() {
  const theme = useTheme();
  return { backgroundColor: theme.color.surface2, borderRadius: radius.lg, padding: spacing.lg };
}

export function Card({ style, ...rest }: CardProps) {
  const base = useCardStyle();
  return <View style={[base, style]} {...rest} />;
}

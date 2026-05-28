import type { ReactNode } from 'react';
import { View } from 'react-native';
import type { StyleProp, ViewStyle } from 'react-native';

import type { Theme } from '../theme/theme';
import { radius, spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';
import { Text } from './Text';

export type BannerTone = 'warning' | 'danger' | 'info';

export type BannerProps = {
  children: ReactNode;
  tone?: BannerTone;
  style?: StyleProp<ViewStyle>;
  testID?: string;
};

function edgeColor(theme: Theme, tone: BannerTone): string {
  switch (tone) {
    case 'warning':
      return theme.color.warning;
    case 'danger':
      return theme.color.danger;
    case 'info':
      return theme.color.accent;
  }
}

/** Inline status banner with a tone-colored left edge (partial-results / errors).
 * A string child is wrapped in Text; any other node renders as-is. */
export function Banner({ children, tone = 'info', style, testID }: BannerProps) {
  const theme = useTheme();
  return (
    <View
      testID={testID}
      style={[
        {
          backgroundColor: theme.color.surface1,
          borderLeftWidth: 3,
          borderLeftColor: edgeColor(theme, tone),
          borderRadius: radius.md,
          paddingVertical: spacing.md,
          paddingHorizontal: spacing.md,
        },
        style,
      ]}
    >
      {typeof children === 'string' ? (
        <Text variant="label" tone="secondary">
          {children}
        </Text>
      ) : (
        children
      )}
    </View>
  );
}

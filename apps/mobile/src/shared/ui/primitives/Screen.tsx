import type { ReactNode } from 'react';
import { View } from 'react-native';
import type { StyleProp, ViewStyle } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

export const SCREEN_HORIZONTAL_PADDING = spacing.lg;

export type ScreenProps = {
  children: ReactNode;
  padded?: boolean;
  style?: StyleProp<ViewStyle>;
  testID?: string;
};

export function Screen({ children, padded = true, style, testID }: ScreenProps) {
  const theme = useTheme();
  const insets = useSafeAreaInsets();
  return (
    <View
      testID={testID}
      style={[
        { flex: 1, backgroundColor: theme.color.canvas, paddingTop: insets.top },
        padded ? { paddingHorizontal: SCREEN_HORIZONTAL_PADDING } : null,
        style,
      ]}
    >
      {children}
    </View>
  );
}

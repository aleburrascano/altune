import type { ReactNode } from 'react';
import { View } from 'react-native';
import type { StyleProp, ViewStyle } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

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
        padded ? { paddingHorizontal: spacing.lg } : null,
        style,
      ]}
    >
      {children}
    </View>
  );
}

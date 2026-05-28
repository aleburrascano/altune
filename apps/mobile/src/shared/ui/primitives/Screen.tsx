import type { ReactNode } from 'react';
import { View } from 'react-native';
import type { StyleProp, ViewStyle } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

export type ScreenProps = {
  children: ReactNode;
  /** Apply the standard horizontal screen padding (default true). */
  padded?: boolean;
  style?: StyleProp<ViewStyle>;
  testID?: string;
};

/** Canvas-colored, safe-area-aware page frame. Screens add their own scroller
 * (FlatList / ScrollView) inside. */
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

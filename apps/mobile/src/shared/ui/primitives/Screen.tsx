import type { ReactNode } from 'react';
import { View } from 'react-native';
import type { StyleProp, ViewStyle } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { useTheme } from '../theme/useTheme';
import { CONTENT_MAX_WIDTH, useLayoutMode } from '../layout/useLayoutMode';
import { SCREEN_HORIZONTAL_PADDING } from './screenLayout';

export type ScreenProps = {
  children: ReactNode;
  padded?: boolean;
  style?: StyleProp<ViewStyle>;
  testID?: string;
};

export function Screen({ children, padded = true, style, testID }: ScreenProps) {
  const theme = useTheme();
  const insets = useSafeAreaInsets();
  const layoutMode = useLayoutMode();
  const isWide = layoutMode === 'wide';
  return (
    <View
      testID={testID}
      style={[
        { flex: 1, backgroundColor: theme.color.canvas, paddingTop: insets.top },
        isWide ? { alignItems: 'center' } : null,
        padded ? { paddingHorizontal: SCREEN_HORIZONTAL_PADDING } : null,
        style,
      ]}
    >
      <View style={isWide ? { width: '100%', maxWidth: CONTENT_MAX_WIDTH, flex: 1 } : { flex: 1, width: '100%' }}>
        {children}
      </View>
    </View>
  );
}

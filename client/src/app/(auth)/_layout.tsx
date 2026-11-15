import { Stack } from 'expo-router';
import { View } from 'react-native';

import { ArtworkBackground, ScreenBoundary, themes, useThemePreference } from '@shared/ui';

export default function AuthLayout() {
  const scheme = useThemePreference((s) => s.scheme);
  return (
    <View style={{ flex: 1, backgroundColor: themes[scheme].color.canvas }}>
      <ArtworkBackground />
      <ScreenBoundary>
        <Stack
          screenOptions={{
            headerShown: false,
            animation: 'fade',
            contentStyle: { backgroundColor: 'transparent' },
          }}
        />
      </ScreenBoundary>
    </View>
  );
}

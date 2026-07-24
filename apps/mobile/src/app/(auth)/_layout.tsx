import { Stack } from 'expo-router';
import { View } from 'react-native';

import { ArtworkBackground } from '@features/auth/ui/hero/ArtworkBackground';
import { ScreenBoundary } from '@shared/ui/ScreenBoundary';
import { themes } from '@shared/ui/theme';
import { useThemePreference } from '@shared/ui/theme/themePreference';

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

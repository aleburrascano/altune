import type { ReactNode } from 'react';
import { Platform, ScrollView, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Text } from '@shared/ui/primitives/Text';
import { Wordmark } from '@shared/ui/primitives/Wordmark';
import { spacing, useTheme } from '@shared/ui/theme';

import { ArtworkBackground } from './ArtworkBackground';
import { EqGlyph } from './EqGlyph';

type AuthHeroLayoutProps = {
  tagline?: string;
  children: ReactNode;
  background?: boolean;
  testID?: string;
};

export function AuthHeroLayout({
  tagline,
  children,
  background = true,
  testID,
}: AuthHeroLayoutProps) {
  const theme = useTheme();
  const insets = useSafeAreaInsets();

  return (
    <View
      testID={testID}
      style={[styles.root, { backgroundColor: background ? theme.color.canvas : 'transparent' }]}
    >
      {background ? <ArtworkBackground /> : null}
      <View pointerEvents="box-none" style={[styles.hero, { top: insets.top + spacing['4xl'] }]}>
        <EqGlyph />
        <Wordmark size={34} />
        {tagline ? (
          <Text variant="label" tone="secondary">
            {tagline}
          </Text>
        ) : null}
      </View>
      <ScrollView
        style={styles.scroll}
        contentContainerStyle={[
          styles.content,
          { paddingTop: insets.top + spacing['4xl'], paddingBottom: insets.bottom + spacing.xl },
        ]}
        keyboardShouldPersistTaps="handled"
        keyboardDismissMode="interactive"
        showsVerticalScrollIndicator={false}
        bounces={false}
        automaticallyAdjustKeyboardInsets={Platform.OS === 'ios'}
      >
        <View>{children}</View>
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  hero: {
    position: 'absolute',
    left: spacing.xl,
    right: spacing.xl,
    gap: spacing.sm,
    zIndex: 1,
  },
  scroll: { flex: 1 },
  content: { flexGrow: 1, justifyContent: 'flex-end', paddingHorizontal: spacing.xl },
});

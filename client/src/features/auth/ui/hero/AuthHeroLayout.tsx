import type { ReactNode } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { adjustsKeyboardInsets } from '@shared/ui/keyboard/keyboard';
import { Text } from '@shared/ui/primitives/Text';
import { Wordmark } from '@shared/ui/primitives/Wordmark';
import { spacing, useTheme } from '@shared/ui/theme';

import { ArtworkBackground } from '@features/auth/ui/hero/ArtworkBackground';

const EQ_BARS = [
  { id: 'a', height: 7 },
  { id: 'b', height: 14 },
  { id: 'c', height: 6 },
  { id: 'd', height: 12 },
  { id: 'e', height: 16 },
];

const HIDDEN_FROM_A11Y = {
  accessibilityElementsHidden: true,
  importantForAccessibility: 'no-hide-descendants',
} as const;

const EQ_BAR_WIDTH = 3;
const EQ_BAR_RADIUS = 2;
const EQ_GLYPH_HEIGHT = 16;

const EQ_GLYPH_BAR_GAP = 3;

const eqStyles = StyleSheet.create({
  glyph: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    gap: EQ_GLYPH_BAR_GAP,
    height: EQ_GLYPH_HEIGHT,
  },
  bar: { width: EQ_BAR_WIDTH, borderRadius: EQ_BAR_RADIUS },
});

type EqBarProps = { height: number };

function EqBar({ height }: EqBarProps) {
  const theme = useTheme();
  return <View style={[eqStyles.bar, { height, backgroundColor: theme.color.accent }]} />;
}

function EqGlyph() {
  return (
    <View {...HIDDEN_FROM_A11Y} style={eqStyles.glyph}>
      {EQ_BARS.map((bar) => (
        <EqBar key={bar.id} height={bar.height} />
      ))}
    </View>
  );
}

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
        testID="auth-hero-scroll"
        style={styles.scroll}
        contentContainerStyle={[
          styles.content,
          { paddingTop: insets.top + spacing['4xl'], paddingBottom: insets.bottom + spacing.xl },
        ]}
        keyboardShouldPersistTaps="handled"
        keyboardDismissMode="interactive"
        showsVerticalScrollIndicator={false}
        bounces={false}
        automaticallyAdjustKeyboardInsets={adjustsKeyboardInsets}
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

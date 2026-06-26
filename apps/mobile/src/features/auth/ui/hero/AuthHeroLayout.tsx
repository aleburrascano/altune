import type { ReactNode } from 'react';
import { Platform, ScrollView, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Text } from '@shared/ui/primitives/Text';
import { Wordmark } from '@shared/ui/primitives/Wordmark';
import { spacing, useTheme } from '@shared/ui/theme';

import { ArtworkBackground } from './ArtworkBackground';
import { EqGlyph } from './EqGlyph';

type AuthHeroLayoutProps = {
  /** Hero subtitle under the wordmark (per-screen copy). */
  tagline?: string;
  /** The form / content, bottom-anchored under the hero. */
  children: ReactNode;
  /**
   * Render the artwork background. False for screens inside the (auth) group
   * where the group layout draws ONE persistent background (so navigating
   * between screens never remounts it — no flash). True (default) for
   * standalone screens like the top-level reset-password route.
   */
  background?: boolean;
  testID?: string;
};

/**
 * Shared shell for every auth screen: artwork wall + veil, the wordmark / EQ
 * glyph / tagline pinned near the top (never moves), and the form
 * bottom-anchored in a scroll view.
 *
 * Keyboard handling: the form lives in a ScrollView with
 * `automaticallyAdjustKeyboardInsets` (iOS) — the keyboard insets the scroll
 * and only the FOCUSED field scrolls into view, instead of flinging the whole
 * form up. The hero stays absolutely pinned. On Android the same scroll +
 * `softwareKeyboardLayoutMode: "resize"` (app.json) brings the focused field
 * into view without panning the window.
 */
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
      style={[
        styles.root,
        { backgroundColor: background ? theme.color.canvas : 'transparent' },
      ]}
    >
      {background ? <ArtworkBackground /> : null}
      {/* Pinned: the keyboard must never shift the background or hero. */}
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
  // flexGrow + flex-end keeps the form bottom-anchored when it doesn't fill
  // the screen; it scrolls only once the keyboard inset makes it overflow.
  content: { flexGrow: 1, justifyContent: 'flex-end', paddingHorizontal: spacing.xl },
});

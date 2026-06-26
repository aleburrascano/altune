import type { ReactNode } from 'react';
import { useEffect, useState } from 'react';
import { Keyboard, KeyboardAvoidingView, Platform, StyleSheet, View } from 'react-native';
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

/** True only while the soft keyboard is on screen. */
function useKeyboardOpen(): boolean {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const show = Keyboard.addListener('keyboardDidShow', () => setOpen(true));
    const hide = Keyboard.addListener('keyboardDidHide', () => setOpen(false));
    return () => {
      show.remove();
      hide.remove();
    };
  }, []);
  return open;
}

/**
 * Shared shell for every auth screen: artwork wall + veil, the wordmark / EQ
 * glyph / tagline pinned near the top (never moves), and the form
 * bottom-anchored. Only the form lifts for the keyboard; the safe-area bottom
 * padding is dropped while the keyboard is up so the form doesn't overshoot.
 */
export function AuthHeroLayout({
  tagline,
  children,
  background = true,
  testID,
}: AuthHeroLayoutProps) {
  const theme = useTheme();
  const insets = useSafeAreaInsets();
  const keyboardOpen = useKeyboardOpen();

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
      {/* Only the form lifts above the keyboard. */}
      <KeyboardAvoidingView
        style={styles.kav}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      >
        <View
          style={[
            styles.form,
            { paddingBottom: keyboardOpen ? spacing.md : insets.bottom + spacing.xl },
          ]}
        >
          {children}
        </View>
      </KeyboardAvoidingView>
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
  },
  kav: { flex: 1, justifyContent: 'flex-end' },
  form: { paddingHorizontal: spacing.xl },
});

import type { ReactNode } from 'react';
import { KeyboardAvoidingView, Platform, StyleSheet, View } from 'react-native';
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
  testID?: string;
};

/**
 * Shared shell for every auth screen: full-bleed artwork wall + veil, the
 * wordmark / EQ glyph / tagline pinned near the top, and the screen's form
 * bottom-anchored with a safe-area inset so the trailing link clears the
 * bezel. The hero auto-fills whatever space the form doesn't use.
 */
export function AuthHeroLayout({ tagline, children, testID }: AuthHeroLayoutProps) {
  const theme = useTheme();
  const insets = useSafeAreaInsets();

  return (
    <View testID={testID} style={[styles.root, { backgroundColor: theme.color.canvas }]}>
      {/* Background + hero are pinned: the keyboard must not shift them. */}
      <ArtworkBackground />
      <View
        pointerEvents="box-none"
        style={[styles.hero, { top: insets.top + spacing['4xl'] }]}
      >
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
        <View style={[styles.form, { paddingBottom: insets.bottom + spacing.xl }]}>
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

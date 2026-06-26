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
      <ArtworkBackground />
      <KeyboardAvoidingView
        style={styles.flex}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      >
        <View
          style={[
            styles.body,
            {
              paddingTop: insets.top + spacing['4xl'],
              paddingBottom: insets.bottom + spacing.xl,
            },
          ]}
        >
          <View style={styles.hero}>
            <EqGlyph />
            <Wordmark size={34} />
            {tagline ? (
              <Text variant="label" tone="secondary">
                {tagline}
              </Text>
            ) : null}
          </View>
          <View>{children}</View>
        </View>
      </KeyboardAvoidingView>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  flex: { flex: 1 },
  body: {
    flex: 1,
    justifyContent: 'space-between',
    paddingHorizontal: spacing.xl,
  },
  hero: { gap: spacing.sm },
});

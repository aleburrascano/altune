import { Text as RNText } from 'react-native';
import type { TextProps as RNTextProps } from 'react-native';

import type { Theme } from '../theme/theme';
import { typography } from '../theme/tokens';
import type { TypographyVariant } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

export type TextTone =
  | 'primary'
  | 'secondary'
  | 'tertiary'
  | 'accent'
  | 'onAccent'
  | 'danger'
  | 'success'
  | 'warning';

export type TextProps = RNTextProps & {
  variant?: TypographyVariant;
  tone?: TextTone;
};

function toneColor(theme: Theme, tone: TextTone): string {
  switch (tone) {
    case 'primary':
      return theme.color.textPrimary;
    case 'secondary':
      return theme.color.textSecondary;
    case 'tertiary':
      return theme.color.textTertiary;
    case 'accent':
      return theme.color.accent;
    case 'onAccent':
      return theme.color.onAccent;
    case 'danger':
      return theme.color.danger;
    case 'success':
      return theme.color.success;
    case 'warning':
      return theme.color.warning;
  }
}

/** Typed typography. Sets fontFamily per weight (never fontWeight) so the
 * @expo-google-fonts weighted families render without faux-bolding. */
export function Text({ variant = 'body', tone = 'primary', style, ...rest }: TextProps) {
  const theme = useTheme();
  return (
    <RNText style={[typography[variant], { color: toneColor(theme, tone) }, style]} {...rest} />
  );
}

import { Text as RNText } from 'react-native';
import type { TextProps as RNTextProps } from 'react-native';

import type { Theme } from '@shared/ui/core/theme/theme';
import { typography } from '@shared/ui/core/theme/tokens';
import type { TypographyVariant } from '@shared/ui/core/theme/tokens';
import { useTheme } from '@shared/ui/view/theme/useTheme';

export type TextTone =
  'primary' | 'secondary' | 'tertiary' | 'accent' | 'onAccent' | 'danger' | 'success' | 'warning';

export type TextProps = RNTextProps & {
  variant?: TypographyVariant;
  tone?: TextTone;
};

type ToneColorKey =
  | 'textPrimary'
  | 'textSecondary'
  | 'textTertiary'
  | 'accentText'
  | 'onAccent'
  | 'danger'
  | 'success'
  | 'warning';

const TONE_COLOR: Record<TextTone, ToneColorKey> = {
  primary: 'textPrimary',
  secondary: 'textSecondary',
  tertiary: 'textTertiary',
  accent: 'accentText',
  onAccent: 'onAccent',
  danger: 'danger',
  success: 'success',
  warning: 'warning',
};

function toneColor(theme: Theme, tone: TextTone): string {
  return theme.color[TONE_COLOR[tone]];
}

export function Text({ variant = 'body', tone = 'primary', style, ...rest }: TextProps) {
  const theme = useTheme();
  return (
    <RNText style={[typography[variant], { color: toneColor(theme, tone) }, style]} {...rest} />
  );
}

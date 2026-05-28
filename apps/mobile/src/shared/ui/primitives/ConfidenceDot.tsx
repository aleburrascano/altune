import { View } from 'react-native';

import { confidenceColor } from '../theme/confidenceColor';
import type { ConfidenceLevel } from '../theme/theme';
import { useTheme } from '../theme/useTheme';

const LABELS: Record<ConfidenceLevel, string> = {
  high: 'High confidence',
  medium: 'Medium confidence',
  low: 'Low confidence',
};

export type ConfidenceDotProps = {
  level: ConfidenceLevel;
  size?: number;
};

/** Dedup-confidence indicator. Color comes from the theme's semantic conf-*
 * roles (kept off the brand accent), and the level is announced to a11y. */
export function ConfidenceDot({ level, size = 8 }: ConfidenceDotProps) {
  const theme = useTheme();
  return (
    <View
      accessible
      accessibilityLabel={LABELS[level]}
      style={{
        width: size,
        height: size,
        borderRadius: size / 2,
        backgroundColor: confidenceColor(theme, level),
      }}
    />
  );
}

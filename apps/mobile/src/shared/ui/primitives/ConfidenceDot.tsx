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

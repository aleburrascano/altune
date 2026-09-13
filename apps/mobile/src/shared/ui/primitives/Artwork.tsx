import { Image } from 'expo-image';

import { radius as radiusTokens } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

const ARTWORK_PLACEHOLDER = require('../../../../assets/artwork-placeholder.png');

export type ArtworkProps = {
  uri: string | null;
  size?: number;
  radius?: number;
  accessibilityLabel?: string;
};

export function Artwork({
  uri,
  size = 56,
  radius = radiusTokens.md,
  accessibilityLabel,
}: ArtworkProps) {
  const theme = useTheme();
  return (
    <Image
      // Remount on uri change so a recycled instance never keeps the prior
      // track's bitmap when switching to a coverless track (source={null}).
      key={uri ?? 'placeholder'}
      testID="artwork"
      source={uri != null ? { uri } : ARTWORK_PLACEHOLDER}
      placeholder={ARTWORK_PLACEHOLDER}
      style={{
        width: size,
        height: size,
        borderRadius: radius,
        backgroundColor: theme.color.surface2,
      }}
      contentFit="cover"
      transition={150}
      {...(accessibilityLabel != null ? { accessibilityLabel } : {})}
    />
  );
}

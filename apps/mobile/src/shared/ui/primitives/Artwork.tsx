import { Image } from 'expo-image';

import { radius as radiusTokens } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

export type ArtworkProps = {
  /** Remote image URL, or null to show the placeholder surface. */
  uri: string | null;
  size?: number;
  radius?: number;
  accessibilityLabel?: string;
};

/** Cached, rounded album/artist art (expo-image). Falls back to a surface tile
 * when the source is null. */
export function Artwork({
  uri,
  size = 56,
  radius = radiusTokens.md,
  accessibilityLabel,
}: ArtworkProps) {
  const theme = useTheme();
  return (
    <Image
      source={uri != null ? { uri } : null}
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

import { Image } from 'expo-image';

import { radius as radiusTokens } from '@shared/ui/core/theme/tokens';
import { useTheme } from '@shared/ui/view/theme/useTheme';

const ARTWORK_PLACEHOLDER = require('@assets/artwork-placeholder.png');

export type ArtworkProps = {
  uri: string | null;
  size?: number;
  radius?: number;
  accessibilityLabel?: string;
};

function artworkSource(uri: string | null) {
  return uri != null ? { uri } : ARTWORK_PLACEHOLDER;
}

export function Artwork({
  uri,
  size = 56,
  radius = radiusTokens.md,
  accessibilityLabel,
}: ArtworkProps) {
  const theme = useTheme();
  return (
    <Image
      key={uri ?? 'placeholder'}
      testID="artwork"
      source={artworkSource(uri)}
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

import type { ReactNode } from 'react';
import type { FeaturedArtist } from '@shared/catalog';
import { withFeaturing } from '@shared/catalog';
import { Text, Tappable } from '@shared/ui';
import type { LateralNavHandle } from '@features/detail/view/useDetailNavigation';

import { sharedStyles } from '@features/detail/view/detailStyles';

type SecondaryLineArgs = {
  isArtist: boolean;
  artist: string | null;
  albumCollaborators: FeaturedArtist[];
  lateralNav: LateralNavHandle;
};

export function secondaryLine({
  isArtist,
  artist,
  albumCollaborators,
  lateralNav,
}: SecondaryLineArgs): ReactNode {
  if (isArtist || artist === null) {
    return null;
  }

  return (
    <Tappable
      action="detail.artist_link.open"
      testID="detail-artist-link"
      onPress={() => void lateralNav.navigateTo(artist, 'artist')}
      disabled={lateralNav.state === 'searching'}
      accessibilityRole="link"
      accessibilityLabel={`View artist ${artist}`}
      accessibilityHint="Opens artist detail"
      style={({ pressed }) => (pressed ? sharedStyles.pressed : null)}
    >
      <Text variant="body" tone="accent" numberOfLines={1}>
        {withFeaturing(artist, albumCollaborators)}
      </Text>
    </Tappable>
  );
}

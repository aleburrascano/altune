import type { ReactNode } from 'react';
import { Pressable } from 'react-native';

import type { FeaturedArtist } from '@shared/api-client/types';
import { withFeaturing } from '@shared/lib/featured';
import { Text } from '@shared/ui/primitives/Text';

import type { LateralNavHandle } from '../hooks/useLateralNav';

import { sharedStyles } from './styles';

const MAX_GENRES = 2;

type SecondaryLineArgs = {
  isArtist: boolean;
  genreTags: string[] | undefined;
  artist: string | null;
  albumCollaborators: FeaturedArtist[];
  lateralNav: LateralNavHandle;
};

export function secondaryLine({
  isArtist,
  genreTags,
  artist,
  albumCollaborators,
  lateralNav,
}: SecondaryLineArgs): ReactNode {
  if (isArtist) {
    const genres = (genreTags ?? []).slice(0, MAX_GENRES);
    if (genres.length === 0) {
      return null;
    }
    return (
      <Text variant="body" tone="secondary" numberOfLines={1}>
        {genres.join(' · ')}
      </Text>
    );
  }

  if (artist === null) {
    return null;
  }

  return (
    <Pressable
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
    </Pressable>
  );
}

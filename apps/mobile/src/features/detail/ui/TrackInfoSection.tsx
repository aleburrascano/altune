import { type ReactElement } from 'react';
import { View } from 'react-native';

import { radius } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { FeaturedArtist } from '@shared/api-client/types';

import type { LateralNavHandle, TrackDetailActions } from '../hooks/useTrackDetailActions';

import { Section } from './Section';
import { TrackNavRow } from './TrackNavRow';

type AlbumRowProps = {
  albumName: string;
  imageUri: string | null;
  searching: boolean;
  onPress: () => void;
};

function AlbumRow({ albumName, imageUri, searching, onPress }: AlbumRowProps): ReactElement {
  return (
    <TrackNavRow
      testID="detail-info-album"
      overline="ALBUM"
      title={albumName}
      imageUri={imageUri}
      imageRadius={radius.sm}
      onPress={onPress}
      disabled={searching}
      accessibilityLabel={`View album ${albumName}`}
      accessibilityHint="Opens album detail"
    />
  );
}

type FeaturedRowsProps = {
  featured: FeaturedArtist[];
  onFeaturedPress: (featuredArtist: FeaturedArtist) => void;
};

function FeaturedRows({ featured, onFeaturedPress }: FeaturedRowsProps): ReactElement {
  return (
    <View testID="detail-info-featuring">
      {featured.map((f) => (
        <TrackNavRow
          key={f.mbid ?? f.name}
          overline="FEATURING"
          title={f.name}
          imageUri={null}
          imageRadius={radius.full}
          onPress={() => onFeaturedPress(f)}
          accessibilityLabel={`Tracks featuring ${f.name}`}
        />
      ))}
    </View>
  );
}

type TrackInfoSectionProps = {
  result: DiscoveryResult;
  actions: Pick<TrackDetailActions, 'albumName' | 'featured' | 'onAlbumPress' | 'onFeaturedPress'>;
  lateralNav: LateralNavHandle;
};

export function TrackInfoSection({
  result: track,
  actions,
  lateralNav,
}: TrackInfoSectionProps): ReactElement | null {
  const { albumName, featured } = actions;
  if (albumName === null && featured.length === 0) return null;
  return (
    <Section label="Details">
      {albumName !== null ? (
        <AlbumRow
          albumName={albumName}
          imageUri={track.image_url}
          searching={lateralNav.state === 'searching'}
          onPress={actions.onAlbumPress}
        />
      ) : null}
      {featured.length > 0 ? (
        <FeaturedRows featured={featured} onFeaturedPress={actions.onFeaturedPress} />
      ) : null}
    </Section>
  );
}

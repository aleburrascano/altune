import { type ReactElement } from 'react';
import { View } from 'react-native';

import { radius } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { FeaturedArtist } from '@shared/api-client/types';

import type { LateralNavHandle } from '../hooks/useLateralNav';
import type { TrackDetailActions } from '../hooks/useTrackDetailActions';

import { Section } from './Section';
import { TrackNavRow } from './TrackNavRow';

type AlbumRowProps = {
  albumName: string;
  imageUri: string | null;
  searching: boolean;
  onPress: () => void;
};

function albumInteraction({ albumName, searching, onPress }: AlbumRowProps) {
  return {
    testID: 'detail-info-album',
    onPress,
    disabled: searching,
    accessibilityLabel: `View album ${albumName}`,
    accessibilityHint: 'Opens album detail',
  };
}

function AlbumRow(props: AlbumRowProps): ReactElement {
  const { albumName, imageUri } = props;
  const nav = { overline: 'ALBUM', title: albumName, imageUri, imageRadius: radius.sm };
  return <TrackNavRow nav={nav} interaction={albumInteraction(props)} />;
}

type FeaturedRowProps = { featuredArtist: FeaturedArtist; onPress: () => void };

function FeaturedRow({ featuredArtist, onPress }: FeaturedRowProps): ReactElement {
  const { name } = featuredArtist;
  const nav = { overline: 'FEATURING', title: name, imageUri: null, imageRadius: radius.full };
  const interaction = { onPress, accessibilityLabel: `Tracks featuring ${name}` };
  return <TrackNavRow nav={nav} interaction={interaction} />;
}

type FeaturedRowsProps = {
  featured: FeaturedArtist[];
  onFeaturedPress: (featuredArtist: FeaturedArtist) => void;
};

function FeaturedRows({ featured, onFeaturedPress }: FeaturedRowsProps): ReactElement {
  return (
    <View testID="detail-info-featuring">
      {featured.map((f) => (
        <FeaturedRow key={f.mbid ?? f.name} featuredArtist={f} onPress={() => onFeaturedPress(f)} />
      ))}
    </View>
  );
}

type TrackInfoSectionProps = {
  track: DiscoveryResult;
  actions: Pick<TrackDetailActions, 'albumName' | 'featured' | 'onAlbumPress' | 'onFeaturedPress'>;
  lateralNav: LateralNavHandle;
};

function albumSlot(props: AlbumRowProps | null): ReactElement | null {
  return props === null ? null : <AlbumRow {...props} />;
}

function featuredSlot(props: FeaturedRowsProps | null): ReactElement | null {
  return props === null ? null : <FeaturedRows {...props} />;
}

function albumSlotProps(props: TrackInfoSectionProps): AlbumRowProps | null {
  const { albumName, onAlbumPress } = props.actions;
  if (albumName === null) return null;
  const searching = props.lateralNav.state === 'searching';
  return { albumName, imageUri: props.track.image_url, searching, onPress: onAlbumPress };
}

function featuredSlotProps(props: TrackInfoSectionProps): FeaturedRowsProps | null {
  const { featured, onFeaturedPress } = props.actions;
  if (featured.length === 0) return null;
  return { featured, onFeaturedPress };
}

export function TrackInfoSection(props: TrackInfoSectionProps): ReactElement | null {
  const album = albumSlotProps(props);
  const featured = featuredSlotProps(props);
  return album === null && featured === null ? null : (
    <Section label="Details">
      {albumSlot(album)}
      {featuredSlot(featured)}
    </Section>
  );
}

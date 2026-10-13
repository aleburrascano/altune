import type { ReactElement } from 'react';
import { View, StyleSheet } from 'react-native';

import { ChevronRight } from 'lucide-react-native';

import { radius, spacing, useTheme } from '@shared/ui/theme';
import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { FeaturedArtist } from '@shared/apiClient/trackResponse';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { Tappable } from '@shared/ui/primitives/Tappable';

import type { LateralNavHandle } from '@features/detail/hooks/useDetailNavigation';
import type { TrackDetailActions } from '@features/detail/hooks/useTrackDetailActions';
import { Section } from '@features/detail/ui/Section';
import { sharedStyles } from '@features/detail/ui/detailStyles';

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

type TrackNavRowNav = {
  overline: string;
  title: string;
  imageUri: string | null;
  imageRadius: number;
};

type TrackNavRowInteraction = {
  onPress: () => void;
  disabled?: boolean;
  testID?: string;
  accessibilityLabel: string;
  accessibilityHint?: string;
};

type TrackNavRowProps = {
  nav: TrackNavRowNav;
  interaction: TrackNavRowInteraction;
};

function pressableStyle(borderColor: string) {
  return ({ pressed }: { pressed: boolean }) => [
    styles.navRow,
    { borderBottomColor: borderColor },
    pressed ? sharedStyles.pressed : null,
  ];
}

const NAV_ARTWORK_SIZE = 36;
const NAV_CHEVRON_SIZE = 16;

function TrackNavRow({ nav, interaction }: TrackNavRowProps): ReactElement {
  const { color } = useTheme();
  return (
    <Tappable
      action="detail.track_nav.open"
      {...interaction}
      accessibilityRole="link"
      style={pressableStyle(color.border)}
    >
      <Artwork uri={nav.imageUri} size={NAV_ARTWORK_SIZE} radius={nav.imageRadius} />
      <NavRowText overline={nav.overline} title={nav.title} />
      <ChevronRight size={NAV_CHEVRON_SIZE} color={color.textTertiary} />
    </Tappable>
  );
}

const overlineTextProps = { variant: 'overline', tone: 'tertiary' } as const;
const titleTextProps = { variant: 'body', numberOfLines: 1 } as const;

function NavRowText({ overline, title }: { overline: string; title: string }): ReactElement {
  return (
    <View style={styles.navText}>
      <Text {...overlineTextProps}>{overline}</Text>
      <Text {...titleTextProps}>{title}</Text>
    </View>
  );
}

const TRACK_INFO_MIN_HEIGHT = 56;

const styles = StyleSheet.create({
  navRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    minHeight: TRACK_INFO_MIN_HEIGHT,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  navText: { flex: 1 },
});

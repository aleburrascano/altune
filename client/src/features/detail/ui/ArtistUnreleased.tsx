import { useState, type ReactElement } from 'react';
import { Linking, StyleSheet, View } from 'react-native';

import { ModalFrame } from '@shared/ui/primitives/ModalFrame';

import { HorizontalRail } from '@shared/ui/primitives/HorizontalRail';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme';
import { useTheme } from '@shared/ui/theme/useTheme';
import { sheetGrabber, sheetTopRadius } from '@shared/ui/theme/tokens';

import { realPhotoUrl } from '@features/detail/domain/artistPhoto';
import { hasManyVersions } from '@features/detail/domain/unreleasedTrack';
import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedTrack, UnreleasedVersion } from '@shared/apiClient/enrichment';

import {
  useUnreleasedCard,
  type UnreleasedCard,
  type UnreleasedCardState,
  useUnreleasedTracks,
} from '@features/detail/hooks/useUnreleasedTracks';
import { FilterChips, type ChipGroup } from '@features/detail/ui/DiscographySections';
import { DETAIL_GUTTER, RELATED_CARD_WIDTH, sharedStyles } from '@features/detail/ui/detailStyles';
import { AlbumCardsSkeleton } from '@features/detail/ui/DetailSkeleton';
import { Section, SectionError } from '@features/detail/ui/Section';

type UnreleasedVersionsSheetProps = {
  track: UnreleasedTrack;
  visible: boolean;
  onClose: () => void;
};
type UnreleasedRowProps = { track: UnreleasedTrack };
type UnreleasedArtistRowProps = { artist: DiscoveryResult; track: UnreleasedTrack };
type UnreleasedCardProps = { track: UnreleasedTrack; card: UnreleasedCard };
type UnreleasedListProps = { artist: DiscoveryResult; tracks: UnreleasedTrack[] };
type ArtistUnreleasedProps = { artist: DiscoveryResult };
type RetryProps = { onRetry: () => void };
type PressState = { pressed: boolean };

type HintOf = (track: UnreleasedTrack) => string | null;

function noHint(): null {
  return null;
}

function fixedHint(text: string): HintOf {
  return function hint() {
    return text;
  };
}

function geniusHint(track: UnreleasedTrack): string | null {
  return track.has_media === false ? 'no audio found' : null;
}

function openHint(track: UnreleasedTrack): string {
  return `Open on ${track.open_host ?? 'link'}`;
}

const CARD_HINTS: Record<UnreleasedCardState, HintOf> = {
  genius: geniusHint,
  open: openHint,
  add: fixedHint('Tap to save'),
  saving: fixedHint('Downloading…'),
  ready: noHint,
  play: fixedHint('Tap to play'),
  failed: fixedHint("Couldn't get it · tap to retry"),
  rejected: fixedHint("Couldn't get it"),
  disabled: noHint,
};

function cardHint(track: UnreleasedTrack, state: UnreleasedCardState): string | null {
  return CARD_HINTS[state](track);
}

function VersionsHint({ track }: UnreleasedRowProps): ReactElement | null {
  if (!hasManyVersions(track)) return null;
  return (
    <Text variant="label" tone="tertiary" testID={`detail-unreleased-versions-${track.key}`}>
      {`${track.versions.length} versions`}
    </Text>
  );
}

function CardHint({ track, card }: UnreleasedCardProps): ReactElement | null {
  const hint = cardHint(track, card.state);
  if (hint === null) return null;
  return (
    <Text variant="label" tone="tertiary" testID={`detail-unreleased-hint-${track.key}`}>
      {hint}
    </Text>
  );
}

type UnreleasedCoverProps = { track: UnreleasedTrack; artist: DiscoveryResult };

function UnreleasedCover({ track, artist }: UnreleasedCoverProps): ReactElement {
  return (
    <Artwork
      uri={track.cover_url ?? realPhotoUrl(artist.image_url)}
      size={RELATED_CARD_WIDTH}
      radius={radius.md}
      accessibilityLabel={track.title}
    />
  );
}

function QualityLine({ track }: UnreleasedRowProps): ReactElement | null {
  if (track.quality === null) return null;
  return (
    <Text variant="label" tone="secondary" testID={`detail-unreleased-quality-${track.key}`}>
      {track.quality}
    </Text>
  );
}

function UnreleasedTitle({ track, card }: UnreleasedCardProps): ReactElement {
  return (
    <View style={styles.title}>
      <Text variant="body" numberOfLines={1}>
        {track.title}
      </Text>
      <QualityLine track={track} />
      <CardHint track={track} card={card} />
      <VersionsHint track={track} />
    </View>
  );
}

function pressedCardStyle({ pressed }: PressState) {
  return [styles.card, pressed ? sharedStyles.pressed : null];
}

function openGenius(track: UnreleasedTrack): void {
  if (track.genius_url === null) return;
  Linking.openURL(track.genius_url).catch((error: unknown) => {
    console.warn('[detail] could not open the Genius page', {
      geniusId: track.genius_id,
      error: error instanceof Error ? error.message : String(error),
    });
  });
}

function openLink(track: UnreleasedTrack): void {
  if (track.open_url === null) return;
  Linking.openURL(track.open_url).catch((error: unknown) => {
    console.warn('[detail] could not open the track link', {
      key: track.key,
      error: error instanceof Error ? error.message : String(error),
    });
  });
}

type LabelOf = (track: UnreleasedTrack) => string;

function saveLabel(track: UnreleasedTrack): string {
  return `Save ${track.title}`;
}

function geniusLabel(track: UnreleasedTrack): string {
  return `Open ${track.title} on Genius`;
}

function openLabel(track: UnreleasedTrack): string {
  return `Open ${track.title} on ${track.open_host ?? 'link'}`;
}

function playLabel(track: UnreleasedTrack): string {
  return `Play ${track.title}`;
}

const CARD_LABELS: Record<UnreleasedCardState, LabelOf> = {
  genius: geniusLabel,
  open: openLabel,
  play: playLabel,
  add: saveLabel,
  saving: saveLabel,
  ready: saveLabel,
  failed: saveLabel,
  rejected: saveLabel,
  disabled: saveLabel,
};

function cardLabel(track: UnreleasedTrack, state: UnreleasedCardState): string {
  return CARD_LABELS[state](track);
}

const LINK_STATES: ReadonlySet<UnreleasedCardState> = new Set(['genius', 'open']);

function unreleasedRowProps({ track, card }: UnreleasedCardProps) {
  return {
    action: `detail.artist.unreleased.${card.state}`,
    testID: `detail-unreleased-${track.key}`,
    onPress: card.onPress,
    accessibilityRole: LINK_STATES.has(card.state) ? ('link' as const) : ('button' as const),
    accessibilityLabel: cardLabel(track, card.state),
    style: pressedCardStyle,
  };
}

const CLOSE_ACTION = 'detail.artist.unreleased.versions.close';

type TrackProps = { track: UnreleasedTrack };
type QualityProps = { quality: string | null };
type VersionRowProps = { version: UnreleasedVersion; testID: string };

function QualityText({ quality }: QualityProps): ReactElement | null {
  if (quality === null) return null;
  return (
    <Text variant="label" tone="secondary">
      {quality}
    </Text>
  );
}

function VersionRow({ version, testID }: VersionRowProps): ReactElement {
  return (
    <View style={sheetStyles.row} testID={testID}>
      <Text variant="body">{version.label ?? version.title}</Text>
      <QualityText quality={version.quality} />
    </View>
  );
}

function VersionRows({ track }: TrackProps): ReactElement {
  const rows = track.versions.map((version, index) => (
    <VersionRow
      key={version.title}
      version={version}
      testID={`detail-unreleased-version-${track.key}-${index}`}
    />
  ));
  return <>{rows}</>;
}

function SheetTitle({ track }: TrackProps): ReactElement {
  return (
    <Text variant="title" numberOfLines={2} style={sheetStyles.title}>
      {track.title}
    </Text>
  );
}

function SheetBody({ track }: TrackProps): ReactElement {
  const { color } = useTheme();
  return (
    <View style={[sheetStyles.sheet, { backgroundColor: color.surface1 }]}>
      <View style={[sheetStyles.handle, { backgroundColor: color.border }]} />
      <SheetTitle track={track} />
      <VersionRows track={track} />
    </View>
  );
}

function frameProps({ track, visible, onClose }: UnreleasedVersionsSheetProps) {
  const testID = `detail-unreleased-versions-sheet-${track.key}`;
  return { visible, onClose, testID, variant: 'sheet' as const, closeAction: CLOSE_ACTION };
}

function UnreleasedVersionsSheet(props: UnreleasedVersionsSheetProps): ReactElement {
  return (
    <ModalFrame {...frameProps(props)}>
      <SheetBody track={props.track} />
    </ModalFrame>
  );
}

function useVersionsSheet(track: UnreleasedTrack) {
  const [open, setOpen] = useState(false);
  if (!hasManyVersions(track)) return { onLongPress: undefined, sheet: null };
  const props = { track, visible: open, onClose: () => setOpen(false) };
  return { onLongPress: () => setOpen(true), sheet: <UnreleasedVersionsSheet {...props} /> };
}

function UnreleasedRow({ artist, track }: UnreleasedArtistRowProps): ReactElement {
  const card = useUnreleasedCard({
    track,
    artist,
    openGenius: () => openGenius(track),
    openLink: () => openLink(track),
  });
  const versions = useVersionsSheet(track);
  return (
    <>
      <Tappable {...unreleasedRowProps({ track, card })} onLongPress={versions.onLongPress}>
        <UnreleasedCover track={track} artist={artist} />
        <UnreleasedTitle track={track} card={card} />
      </Tappable>
      {versions.sheet}
    </>
  );
}

function unreleasedKey(track: UnreleasedTrack): string {
  return track.key;
}

type UnreleasedItemInfo = { item: UnreleasedTrack };

function rowRenderer(artist: DiscoveryResult) {
  return function renderUnreleasedRow({ item }: UnreleasedItemInfo): ReactElement {
    return <UnreleasedRow artist={artist} track={item} />;
  };
}

const RAIL_LAYOUT = { testID: 'detail-unreleased-rail', keyExtractor: unreleasedKey };

function UnreleasedRail({ artist, tracks }: UnreleasedListProps): ReactElement {
  const renderItem = rowRenderer(artist);
  return <HorizontalRail {...RAIL_LAYOUT} {...railStyle()} data={tracks} renderItem={renderItem} />;
}

function railStyle() {
  return { style: styles.rail, contentContainerStyle: styles.railContent };
}

const SECTION_LABEL = 'Not on streaming';
const RELEASED = 'released';
const UNRELEASED = 'unreleased';

function trackGroups(tracks: UnreleasedTrack[]): ChipGroup[] {
  const released = tracks.filter((t) => t.off_streaming).length;
  const groups = [
    { type: RELEASED, label: 'Released', count: released },
    { type: UNRELEASED, label: 'Unreleased', count: tracks.length - released },
  ];
  return groups.filter((g) => g.count > 0);
}

function inGroup(tracks: UnreleasedTrack[], type: string): UnreleasedTrack[] {
  return tracks.filter((t) => t.off_streaming === (type === RELEASED));
}

function useTrackFilter(tracks: UnreleasedTrack[]) {
  const groups = trackGroups(tracks);
  const [picked, setPicked] = useState<string | null>(null);
  const activeType = groups.some((g) => g.type === picked) ? picked : groups[0]?.type;
  return { groups, activeType: activeType ?? UNRELEASED, onSelect: setPicked };
}

const CHIP_IDS = { action: 'detail.unreleased.select', testIDPrefix: 'detail-unreleased-group' };

function FilteredRail({ artist, tracks }: UnreleasedListProps): ReactElement {
  const filter = useTrackFilter(tracks);
  const shown = inGroup(tracks, filter.activeType);
  return (
    <>
      <FilterChips {...filter} {...CHIP_IDS} />
      <UnreleasedRail artist={artist} tracks={shown} />
    </>
  );
}

function UnreleasedSection(props: UnreleasedListProps): ReactElement {
  return (
    <View testID="detail-unreleased">
      <Section label={SECTION_LABEL}>
        <FilteredRail {...props} />
      </Section>
    </View>
  );
}

function UnreleasedLoading(): ReactElement {
  return (
    <View testID="detail-unreleased-loading">
      <Section label={SECTION_LABEL}>
        <AlbumCardsSkeleton />
      </Section>
    </View>
  );
}

const FAILED_MESSAGE = "Couldn't load unreleased tracks";

function UnreleasedFailed({ onRetry }: RetryProps): ReactElement {
  const error = { testIDPrefix: 'detail-unreleased', message: FAILED_MESSAGE, onRetry };
  return (
    <Section label={SECTION_LABEL}>
      <SectionError {...error} />
    </Section>
  );
}

export function ArtistUnreleased({ artist }: ArtistUnreleasedProps): ReactElement | null {
  const { tracks, isLoading, isFailed, retry } = useUnreleasedTracks(artist);
  if (tracks.length > 0) return <UnreleasedSection artist={artist} tracks={tracks} />;
  if (isLoading) return <UnreleasedLoading />;
  return isFailed ? <UnreleasedFailed onRetry={retry} /> : null;
}

const styles = StyleSheet.create({
  rail: { marginHorizontal: -DETAIL_GUTTER },
  railContent: { paddingHorizontal: DETAIL_GUTTER, gap: spacing.md },
  card: { width: RELATED_CARD_WIDTH, gap: spacing.xs },
  title: { marginTop: spacing.xs },
});

const sheetStyles = StyleSheet.create({
  sheet: {
    borderTopLeftRadius: sheetTopRadius,
    borderTopRightRadius: sheetTopRadius,
    paddingHorizontal: spacing.xl,
    paddingBottom: spacing['3xl'],
    paddingTop: spacing.md,
  },
  handle: { ...sheetGrabber, alignSelf: 'center', marginBottom: spacing.lg },
  title: { marginBottom: spacing.md },
  row: { paddingVertical: spacing.md, gap: spacing.xs },
});

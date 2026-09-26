import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { spacing } from '@shared/ui/theme';

import { type ContentFailure } from '../content-status';
import type { ArtistDetailState } from '../hooks/useArtistDetailState';

import { AsyncListSection } from './AsyncListSection';
import { AlbumCardsSkeleton } from './DetailSkeleton';
import { CollapsibleSectionHeader } from './CollapsibleSectionHeader';
import { DiscographySections } from './DiscographySections';

function exploreAccessibilityLabel(expanded: boolean): string {
  return expanded ? 'Collapse discography' : 'Explore full discography';
}

function toggleExplore(artist: ArtistDetailState) {
  return () => artist.setExploreExpanded((prev) => !prev);
}

function exploreHeaderProps(artist: ArtistDetailState) {
  return {
    label: 'Explore Discography',
    expanded: artist.exploreExpanded,
    onToggle: toggleExplore(artist),
    testID: 'detail-explore-discography',
    accessibilityLabel: exploreAccessibilityLabel(artist.exploreExpanded),
  };
}

function exploreFailureFor(artist: ArtistDetailState): ContentFailure | null {
  return artist.discoveryError ? 'transient' : artist.albumsFailure;
}

function exploreErrorProps(artist: ArtistDetailState) {
  return {
    testIDPrefix: 'detail-explore',
    message: "Couldn't load discography.",
    onRetry: () => artist.discoveryRefetch(),
    failure: exploreFailureFor(artist),
  };
}

const EXPLORE_EMPTY = {
  message: 'No additional albums found.',
  variant: 'caption' as const,
  tone: 'tertiary' as const,
};

function exploreAsyncProps(artist: ArtistDetailState) {
  return {
    isLoading: artist.discoveryLoading || artist.isLoadingAlbums,
    isError: artist.discoveryError || artist.isErrorAlbums,
    isEmpty: artist.apiAlbums.length === 0,
    skeleton: AlbumCardsSkeleton,
    error: exploreErrorProps(artist),
    empty: EXPLORE_EMPTY,
  };
}

function ExploreBody({ artist }: { artist: ArtistDetailState }): ReactElement {
  return (
    <AsyncListSection {...exploreAsyncProps(artist)}>
      <DiscographySections albums={artist.apiAlbums} onAlbumPress={artist.onAlbumPress} />
    </AsyncListSection>
  );
}

export function ArtistExploreDiscography({ artist }: { artist: ArtistDetailState }): ReactElement {
  return (
    <View style={styles.exploreSection}>
      <CollapsibleSectionHeader {...exploreHeaderProps(artist)} />
      {artist.exploreExpanded ? <ExploreBody artist={artist} /> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  exploreSection: { marginTop: spacing.xl },
});

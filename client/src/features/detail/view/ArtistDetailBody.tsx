import { type ReactElement } from 'react';
import { View } from 'react-native';

import { Play } from 'lucide-react-native';

import { Text } from '@shared/ui';

import type { DiscoveryResult } from '@shared/catalog';
import {
  useArtistDetailState,
  type ArtistDetailState,
} from '@features/detail/view/useArtistDetailState';
import type { DetailRoute } from '@features/detail/domain/detailNavigation';

import { DetailFacts } from '@features/detail/view/DetailFacts';
import { buildArtistFacts } from '@features/detail/core/buildDetailFacts';
import { AsyncListSection, Section } from '@features/detail/view/Section';
import { AlbumCardsSkeleton } from '@features/detail/view/DetailSkeleton';
import { ArtistExploreDiscography } from '@features/detail/view/ArtistExploreDiscography';
import { ArtistTopTracks } from '@features/detail/view/ArtistTopTracks';
import { DetailActions } from '@features/detail/view/DetailActions';
import { DetailScaffold, type DetailChrome } from '@features/detail/view/DetailScaffold';
import { DiscographySections } from '@features/detail/view/DiscographySections';
import { ArtistUnreleased } from '@features/detail/view/unreleased/ArtistUnreleased';

type ArtistDetailBodyProps = {
  chrome: DetailChrome;
  result: DiscoveryResult;
  detailRoute: DetailRoute;
};

function artistFactsFor(artist: ArtistDetailState) {
  const releases = artist.hasSources ? artist.apiAlbums.length : artist.libraryAlbums.length;
  const inLibrary = artist.owned.playable.length + artist.owned.acquiringCount;
  return buildArtistFacts({ releases, inLibrary });
}

function artistPrimaryAction(artist: ArtistDetailState) {
  return {
    label: artist.playButton.label,
    icon: Play,
    onPress: artist.onPlayOwned,
    disabled: artist.playButton.disabled,
    testID: 'detail-artist-play',
    accessibilityLabel: artist.playButton.label,
  };
}

function scaffoldContentProps(artist: ArtistDetailState) {
  return {
    facts: <DetailFacts facts={artistFactsFor(artist)} testID="detail-artist-facts" />,
    actions: <DetailActions primary={artistPrimaryAction(artist)} />,
  };
}

function ApiDiscographySkeleton(): ReactElement {
  return (
    <View testID="detail-albums-loading">
      <AlbumCardsSkeleton />
    </View>
  );
}

function apiDiscographyErrorProps(artist: ArtistDetailState) {
  return {
    testIDPrefix: 'detail-albums',
    message: "Couldn't load albums.",
    onRetry: () => artist.refetchAlbums(),
    failure: artist.albumsFailure,
  };
}

const API_DISCOGRAPHY_EMPTY = {
  message: 'No albums found.',
  variant: 'body' as const,
  tone: 'tertiary' as const,
};

function apiDiscographyProps(artist: ArtistDetailState) {
  return {
    isLoading: artist.isLoadingAlbums,
    isError: artist.isErrorAlbums,
    isEmpty: artist.apiAlbums.length === 0,
    skeleton: ApiDiscographySkeleton,
    error: apiDiscographyErrorProps(artist),
    empty: API_DISCOGRAPHY_EMPTY,
  };
}

function ApiDiscography({ artist }: { artist: ArtistDetailState }): ReactElement {
  return (
    <Section label="Discography">
      <AsyncListSection {...apiDiscographyProps(artist)}>
        <DiscographySections albums={artist.apiAlbums} onAlbumPress={artist.onAlbumPress} />
      </AsyncListSection>
    </Section>
  );
}

function LibraryAlbums({ artist }: { artist: ArtistDetailState }): ReactElement | null {
  if (artist.hasSources || artist.libraryAlbums.length === 0) return null;
  return (
    <Section label="Your albums">
      <DiscographySections albums={artist.libraryAlbums} onAlbumPress={artist.onAlbumPress} />
    </Section>
  );
}

function ArtistDiscography({ artist }: { artist: ArtistDetailState }): ReactElement {
  return artist.hasSources ? (
    <ApiDiscography artist={artist} />
  ) : (
    <ArtistExploreDiscography artist={artist} />
  );
}

function IncompleteLibraryNotice({ artist }: { artist: ArtistDetailState }): ReactElement | null {
  if (artist.hasSources || artist.libraryTracksComplete) return null;
  return (
    <Text variant="body" tone="tertiary" testID="detail-library-tracks-incomplete">
      Some of your tracks may be missing from this list.
    </Text>
  );
}

function ArtistDetailContentBody(props: ArtistDetailBodyProps & { artist: ArtistDetailState }) {
  return (
    <>
      <ArtistTopTracks artist={props.artist} artistResult={props.result} />
      <IncompleteLibraryNotice artist={props.artist} />
      <LibraryAlbums artist={props.artist} />
      <ArtistDiscography artist={props.artist} />
      <ArtistUnreleased artist={props.result} detailRoute={props.detailRoute} />
    </>
  );
}

function ArtistDetailContent(
  props: ArtistDetailBodyProps & { artist: ArtistDetailState },
): ReactElement {
  return (
    <View testID="detail-artist-content">
      <ArtistDetailContentBody {...props} />
    </View>
  );
}

export function ArtistDetailBody(props: ArtistDetailBodyProps): ReactElement {
  const artist = useArtistDetailState(props.result, props.detailRoute);
  return (
    <DetailScaffold {...props.chrome} {...scaffoldContentProps(artist)}>
      <ArtistDetailContent {...props} artist={artist} />
    </DetailScaffold>
  );
}

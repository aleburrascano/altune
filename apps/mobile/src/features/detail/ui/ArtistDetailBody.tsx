import { type ReactElement } from 'react';
import { View } from 'react-native';

import { Play } from 'lucide-react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { LastFmEnrichmentResponse } from '@shared/api-client/enrichment';

import { useArtistDetailState, type ArtistDetailState } from '../hooks/useArtistDetailState';
import type { DetailRoute } from '../navigation';

import { compactCount } from './formatters';
import { buildArtistFacts } from './artistDetailFacts';
import { AsyncListSection } from './AsyncListSection';
import { AlbumCardsSkeleton } from './DetailSkeleton';
import { ArtistExploreDiscography } from './ArtistExploreDiscography';
import { ArtistTopTracks } from './ArtistTopTracks';
import { DetailActions } from './DetailActions';
import { DetailFacts } from './DetailFacts';
import { DetailScaffold, type DetailChrome } from './DetailScaffold';
import { DiscographySections } from './DiscographySections';
import { LastFmEnrichmentSection } from './LastFmEnrichmentSection';
import { Section } from './Section';

type ArtistDetailBodyProps = {
  chrome: DetailChrome;
  result: DiscoveryResult;
  detailRoute: DetailRoute;
  lastfm?: LastFmEnrichmentResponse | null | undefined;
  lastfmError?: boolean | undefined;
};

function artistFactsFor(artist: ArtistDetailState, lastfm?: LastFmEnrichmentResponse | null) {
  const releases = artist.hasSources ? artist.apiAlbums.length : artist.libraryAlbums.length;
  const inLibrary = artist.owned.playable.length + artist.owned.acquiringCount;
  const listeners = lastfm != null && lastfm.listeners > 0 ? compactCount(lastfm.listeners) : null;
  return buildArtistFacts({ releases, inLibrary, listeners });
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

function scaffoldContentProps(artist: ArtistDetailState, lastfm?: LastFmEnrichmentResponse | null) {
  return {
    facts: <DetailFacts facts={artistFactsFor(artist, lastfm)} testID="detail-artist-facts" />,
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

const API_DISCOGRAPHY_EMPTY = { message: 'No albums found.', variant: 'body' as const, tone: 'tertiary' as const };

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

type AboutArtistProps = {
  lastfm: LastFmEnrichmentResponse | null | undefined;
  lastfmError: boolean;
};

function AboutArtistWithLastfm(props: AboutArtistProps & { lastfm: LastFmEnrichmentResponse }): ReactElement {
  return (
    <View testID="detail-artist-about">
      <Section label="About">
        <LastFmEnrichmentSection enrichment={props.lastfm} isError={props.lastfmError} />
      </Section>
    </View>
  );
}

function AboutArtist(props: AboutArtistProps): ReactElement {
  if (props.lastfm == null) {
    return <LastFmEnrichmentSection enrichment={null} isError={props.lastfmError} />;
  }
  return <AboutArtistWithLastfm {...props} lastfm={props.lastfm} />;
}

function ArtistDetailContent(props: ArtistDetailBodyProps & { artist: ArtistDetailState }): ReactElement {
  return (
    <View testID="detail-artist-content">
      <ArtistTopTracks artist={props.artist} artistResult={props.result} />
      <LibraryAlbums artist={props.artist} />
      <ArtistDiscography artist={props.artist} />
      <AboutArtist lastfm={props.lastfm} lastfmError={props.lastfmError ?? false} />
    </View>
  );
}

export function ArtistDetailBody(props: ArtistDetailBodyProps): ReactElement {
  const artist = useArtistDetailState(props.result, props.detailRoute);
  return (
    <DetailScaffold {...props.chrome} {...scaffoldContentProps(artist, props.lastfm)}>
      <ArtistDetailContent {...props} artist={artist} />
    </DetailScaffold>
  );
}

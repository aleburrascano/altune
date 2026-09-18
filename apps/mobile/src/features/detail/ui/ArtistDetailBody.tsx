import { useState, type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { ChevronDown, ChevronRight, Play } from 'lucide-react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { LastFmEnrichmentResponse } from '@shared/api-client/enrichment';

import { formatDuration } from '@shared/lib/format';

import { type ContentFailure } from '../content-status';
import { trackExtras } from '../extras-accessors';
import { useArtistDetailState, type ArtistDetailState } from '../hooks/useArtistDetailState';
import type { OwnedTrack } from '../hooks/useOwnedTrack';
import type { DetailRoute } from '../navigation';

import { compactCount } from './formatters';
import { sharedStyles } from './styles';
import { AsyncListSection } from './AsyncListSection';
import { AlbumCardsSkeleton, TrackRowsSkeleton } from './DetailSkeleton';
import { DetailActions } from './DetailActions';
import { DetailFacts, type DetailFact } from './DetailFacts';
import { DetailScaffold, type DetailChrome } from './DetailScaffold';
import { DiscographySections } from './DiscographySections';
import { LastFmEnrichmentSection } from './LastFmEnrichmentSection';
import { Section } from './Section';
import { TrackSaveControl } from './TrackSaveControl';

const TRACK_CAP = 5;

export function ArtistDetailBody({
  chrome,
  result,
  detailRoute,
  lastfm,
  lastfmError = false,
}: {
  chrome: DetailChrome;
  result: DiscoveryResult;
  detailRoute: DetailRoute;
  lastfm?: LastFmEnrichmentResponse | null;
  /** True only when the Last.fm fetch failed, which a missing bio alone is not. */
  lastfmError?: boolean;
}): ReactElement {
  const theme = useTheme();
  const artist = useArtistDetailState(result, detailRoute);

  const releases = artist.hasSources ? artist.apiAlbums.length : artist.libraryAlbums.length;
  const inLibrary = artist.owned.playable.length + artist.owned.acquiringCount;
  const listeners = lastfm != null && lastfm.listeners > 0 ? compactCount(lastfm.listeners) : null;

  const facts: (DetailFact | null)[] = [
    releases > 0 ? { label: 'Releases', value: String(releases) } : null,
    inLibrary > 0 ? { label: 'In library', value: String(inLibrary) } : null,
    listeners !== null ? { label: 'Listeners', value: listeners } : null,
  ];

  return (
    <DetailScaffold
      {...chrome}
      facts={<DetailFacts facts={facts} testID="detail-artist-facts" />}
      actions={
        <DetailActions
          primary={{
            label: artist.playButton.label,
            icon: Play,
            onPress: artist.onPlayOwned,
            disabled: artist.playButton.disabled,
            testID: 'detail-artist-play',
            accessibilityLabel: artist.playButton.label,
          }}
        />
      }
    >
      <View testID="detail-artist-content">
        <PopularTracksSection artist={artist} result={result} />

        {!artist.hasSources && artist.libraryAlbums.length > 0 ? (
          <Section label="Your albums">
            <DiscographySections albums={artist.libraryAlbums} onAlbumPress={artist.onAlbumPress} />
          </Section>
        ) : null}

        {artist.hasSources ? (
          <Section label="Discography">{renderApiDiscography()}</Section>
        ) : (
          renderExplore()
        )}

        {lastfm != null ? (
          <View testID="detail-artist-about">
            <Section label="About">
              <LastFmEnrichmentSection enrichment={lastfm} isError={lastfmError} />
            </Section>
          </View>
        ) : (
          // Outside the "About" heading on purpose: a failed fetch must stay
          // distinguishable from an artist with no Last.fm page without the
          // screen growing an empty section for it.
          <LastFmEnrichmentSection enrichment={null} isError={lastfmError} />
        )}
      </View>
    </DetailScaffold>
  );

  function renderApiDiscography(): ReactElement {
    return (
      <AsyncListSection
        isLoading={artist.isLoadingAlbums}
        isError={artist.isErrorAlbums}
        isEmpty={artist.apiAlbums.length === 0}
        skeleton={() => (
          <View testID="detail-albums-loading">
            <AlbumCardsSkeleton />
          </View>
        )}
        error={{
          testIDPrefix: 'detail-albums',
          message: "Couldn't load albums.",
          onRetry: () => artist.refetchAlbums(),
          failure: artist.albumsFailure,
        }}
        empty={{ message: 'No albums found.', variant: 'body', tone: 'tertiary' }}
      >
        <DiscographySections albums={artist.apiAlbums} onAlbumPress={artist.onAlbumPress} />
      </AsyncListSection>
    );
  }

  function renderExplore(): ReactElement {
    return (
      <View style={styles.exploreSection}>
        <Pressable
          testID="detail-explore-discography"
          onPress={() => artist.setExploreExpanded((prev) => !prev)}
          accessibilityRole="button"
          accessibilityLabel={
            artist.exploreExpanded ? 'Collapse discography' : 'Explore full discography'
          }
          style={({ pressed }) => [styles.exploreHeader, pressed ? sharedStyles.pressed : null]}
        >
          <Text variant="label" tone="accent">
            Explore Discography
          </Text>
          {artist.exploreExpanded ? (
            <ChevronDown size={18} color={theme.color.accent} />
          ) : (
            <ChevronRight size={18} color={theme.color.accent} />
          )}
        </Pressable>

        {artist.exploreExpanded ? renderExploreBody() : null}
      </View>
    );
  }

  function renderExploreBody(): ReactElement {
    // A failed discovery *search* is a plain query failure, never a settled
    // content decision, so it keeps its retry.
    const exploreFailure: ContentFailure | null = artist.discoveryError
      ? 'transient'
      : artist.albumsFailure;
    return (
      <AsyncListSection
        isLoading={artist.discoveryLoading || artist.isLoadingAlbums}
        isError={artist.discoveryError || artist.isErrorAlbums}
        isEmpty={artist.apiAlbums.length === 0}
        skeleton={() => <AlbumCardsSkeleton />}
        error={{
          testIDPrefix: 'detail-explore',
          message: "Couldn't load discography.",
          onRetry: () => artist.discoveryRefetch(),
          failure: exploreFailure,
        }}
        empty={{ message: 'No additional albums found.', variant: 'caption', tone: 'tertiary' }}
      >
        <DiscographySections albums={artist.apiAlbums} onAlbumPress={artist.onAlbumPress} />
      </AsyncListSection>
    );
  }
}

// The "Popular tracks" / "Your tracks" section with its optional "Show all"
// toggle. Extracted from ArtistDetailBody so the label/action-spread branches and
// the show-all state live here rather than inflating the parent's complexity.
function PopularTracksSection({
  artist,
  result,
}: {
  artist: ArtistDetailState;
  result: DiscoveryResult;
}): ReactElement {
  const [showAllTracks, setShowAllTracks] = useState(false);
  const visibleTracks =
    artist.hasSources || showAllTracks ? artist.topTracks : artist.topTracks.slice(0, TRACK_CAP);
  const showAllAction =
    !artist.hasSources && !showAllTracks && artist.topTracks.length > TRACK_CAP
      ? {
          action: {
            label: `Show all ${artist.topTracks.length}`,
            onPress: () => setShowAllTracks(true),
            testID: 'detail-show-all-tracks',
          },
        }
      : {};
  return (
    <Section label={artist.hasSources ? 'Popular tracks' : 'Your tracks'} {...showAllAction}>
      <AsyncListSection
        isLoading={artist.isLoadingTracks}
        isError={artist.isErrorTracks}
        isEmpty={artist.topTracks.length === 0}
        skeleton={() => <TrackRowsSkeleton testID="detail-top-tracks-loading" count={5} />}
        error={{
          testIDPrefix: 'detail-top-tracks',
          message: "Couldn't load tracks.",
          onRetry: () => artist.refetchTracks(),
          failure: artist.tracksFailure,
        }}
        empty={{ message: 'No tracks found.', variant: 'body', tone: 'tertiary' }}
      >
        {visibleTracks.map((track, index) => (
          <ArtistTopTrackRow
            key={track.sources[0]?.external_id ?? index}
            track={track}
            index={index}
            owned={artist.ownedFor(track)}
            fallbackArtist={result.title}
            onPress={() => artist.onTrackPress(track)}
            onQuickSave={() => artist.onQuickSave(track)}
          />
        ))}
      </AsyncListSection>
    </Section>
  );
}

// A single "popular/your tracks" row. Extracted from ArtistDetailBody so the
// row's own branches (duration, pressed style, artist fallback) live here rather
// than inflating the parent's control-flow complexity.
function ArtistTopTrackRow({
  track,
  index,
  owned,
  fallbackArtist,
  onPress,
  onQuickSave,
}: {
  track: DiscoveryResult;
  index: number;
  owned: OwnedTrack | null;
  fallbackArtist: string;
  onPress: () => void;
  onQuickSave: () => void;
}): ReactElement {
  const durationSeconds = trackExtras(track.extras).durationSeconds;
  return (
    <Pressable
      testID={`detail-top-track-${index}`}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`Play ${track.title}`}
      style={({ pressed }) => [sharedStyles.trackRow, pressed ? sharedStyles.pressed : null]}
    >
      <Text variant="label" tone="tertiary" style={styles.rank}>
        {index + 1}
      </Text>
      <Artwork uri={track.image_url} size={40} radius={radius.sm} accessibilityLabel={track.title} />
      <View style={sharedStyles.trackInfo}>
        <Text variant="body" numberOfLines={1}>
          {track.title}
        </Text>
      </View>
      {durationSeconds != null ? (
        <Text variant="label" tone="tertiary" style={styles.trackDuration}>
          {formatDuration(durationSeconds)}
        </Text>
      ) : null}
      <TrackSaveControl
        testID={`detail-top-track-save-${index}`}
        owned={owned}
        title={track.title}
        artist={track.subtitle ?? fallbackArtist}
        onPress={onQuickSave}
      />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  rank: { width: 14, textAlign: 'center' },
  trackDuration: { marginRight: spacing.xs, fontVariant: ['tabular-nums'] },
  exploreSection: { marginTop: spacing.xl },
  exploreHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: spacing.md,
    minHeight: 48,
  },
});

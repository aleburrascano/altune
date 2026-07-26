import { useState, type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { ChevronDown, ChevronRight, Play } from 'lucide-react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { LastFmEnrichmentResponse } from '@shared/api-client/enrichment';

import { formatDuration } from '@shared/lib/format';

import { trackExtras } from '../extras-accessors';
import { useArtistDetailState } from '../hooks/useArtistDetailState';
import type { DetailRoute } from '../navigation';

import { compactCount, sharedStyles } from './helpers';
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
  isFromLibrary,
  lastfm,
}: {
  chrome: DetailChrome;
  result: DiscoveryResult;
  detailRoute: DetailRoute;
  isFromLibrary?: boolean;
  lastfm?: LastFmEnrichmentResponse | null;
}): ReactElement {
  const theme = useTheme();
  const artist = useArtistDetailState(result, detailRoute, isFromLibrary);
  const [showAllTracks, setShowAllTracks] = useState(false);
  const visibleTracks =
    artist.hasSources || showAllTracks ? artist.topTracks : artist.topTracks.slice(0, TRACK_CAP);

  const releases = artist.hasSources ? artist.apiAlbums.length : artist.libraryAlbums.length;
  const inLibrary = artist.owned.playable.length + artist.owned.acquiringCount;
  const listeners = lastfm != null && lastfm.listeners > 0 ? compactCount(lastfm.listeners) : null;

  const facts: (DetailFact | null)[] = [
    releases > 0 ? { label: 'Releases', value: String(releases) } : null,
    inLibrary > 0 ? { label: 'In library', value: String(inLibrary) } : null,
    listeners !== null ? { label: 'Listeners', value: listeners } : null,
  ];

  const hasAbout = lastfm != null;

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
        <Section
          label={artist.hasSources ? 'Popular tracks' : 'Your tracks'}
          {...(!artist.hasSources && !showAllTracks && artist.topTracks.length > TRACK_CAP
            ? {
                action: {
                  label: `Show all ${artist.topTracks.length}`,
                  onPress: () => setShowAllTracks(true),
                  testID: 'detail-show-all-tracks',
                },
              }
            : {})}
        >
          {renderTracks()}
        </Section>

        {!artist.hasSources && artist.libraryAlbums.length > 0 ? (
          <Section label="Your albums">
            <DiscographySections albums={artist.libraryAlbums} onAlbumPress={artist.onAlbumPress} />
          </Section>
        ) : null}

        {artist.hasSources ? renderApiDiscography() : renderExplore()}

        {hasAbout ? (
          <View testID="detail-artist-about">
            <Section label="About">
              <LastFmEnrichmentSection kind="artist" enrichment={lastfm} />
            </Section>
          </View>
        ) : null}
      </View>
    </DetailScaffold>
  );

  function renderTracks(): ReactElement {
    if (artist.isLoadingTracks) {
      return <TrackRowsSkeleton testID="detail-top-tracks-loading" count={5} />;
    }
    if (artist.isErrorTracks) {
      return (
        <View testID="detail-top-tracks-error" style={styles.sectionError}>
          <Text variant="body" tone="danger">
            Couldn&apos;t load tracks.
          </Text>
          <Button
            testID="detail-top-tracks-retry"
            label="Retry"
            onPress={() => artist.refetchTracks()}
            style={sharedStyles.retryButton}
          />
        </View>
      );
    }
    if (artist.topTracks.length === 0) {
      return (
        <Text variant="body" tone="tertiary" style={styles.emptySection}>
          No tracks found.
        </Text>
      );
    }
    return (
      <>
        {visibleTracks.map((track, index) => {
          const durationSeconds = trackExtras(track.extras).durationSeconds;
          return (
            <Pressable
              key={track.sources[0]?.external_id ?? index}
              testID={`detail-top-track-${index}`}
              onPress={() => artist.onTrackPress(track)}
              accessibilityRole="button"
              accessibilityLabel={`Play ${track.title}`}
              style={({ pressed }) => [sharedStyles.trackRow, pressed ? styles.pressed : null]}
            >
              <Text variant="label" tone="tertiary" style={styles.rank}>
                {index + 1}
              </Text>
              <Artwork
                uri={track.image_url}
                size={40}
                radius={radius.sm}
                accessibilityLabel={track.title}
              />
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
                state={artist.saveStateFor(track)}
                title={track.title}
                onPress={() => artist.onQuickSave(track)}
              />
            </Pressable>
          );
        })}
      </>
    );
  }

  function renderApiDiscography(): ReactElement {
    if (artist.isLoadingAlbums) {
      return (
        <View testID="detail-albums-loading" style={sharedStyles.albumsSection}>
          <AlbumCardsSkeleton />
        </View>
      );
    }
    if (artist.isErrorAlbums) {
      return (
        <View
          testID="detail-albums-error"
          style={[styles.sectionError, sharedStyles.albumsSection]}
        >
          <Text variant="body" tone="danger">
            Couldn&apos;t load albums.
          </Text>
          <Button
            testID="detail-albums-retry"
            label="Retry"
            onPress={() => artist.refetchAlbums()}
            style={sharedStyles.retryButton}
          />
        </View>
      );
    }
    if (artist.apiAlbums.length === 0) {
      return (
        <Text
          variant="body"
          tone="tertiary"
          style={[styles.emptySection, sharedStyles.albumsSection]}
        >
          No albums found.
        </Text>
      );
    }
    return <DiscographySections albums={artist.apiAlbums} onAlbumPress={artist.onAlbumPress} />;
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
          style={({ pressed }) => [styles.exploreHeader, pressed ? styles.pressed : null]}
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
    if (artist.discoveryLoading || artist.isLoadingAlbums) {
      return <AlbumCardsSkeleton />;
    }
    if (artist.discoveryError || artist.isErrorAlbums) {
      return (
        <View style={styles.sectionError}>
          <Text variant="caption" tone="secondary">
            Couldn&apos;t load discography.
          </Text>
          <Button
            label="Retry"
            onPress={() => artist.refetchAlbums()}
            style={sharedStyles.retryButton}
          />
        </View>
      );
    }
    if (artist.apiAlbums.length === 0) {
      return (
        <Text variant="caption" tone="tertiary" style={styles.emptySection}>
          No additional albums found.
        </Text>
      );
    }
    return <DiscographySections albums={artist.apiAlbums} onAlbumPress={artist.onAlbumPress} />;
  }
}

const styles = StyleSheet.create({
  rank: { width: 14, textAlign: 'center' },
  trackDuration: { marginRight: spacing.xs, fontVariant: ['tabular-nums'] },
  sectionError: { paddingVertical: spacing.md, alignItems: 'center' },
  emptySection: { paddingVertical: spacing.md },
  exploreSection: { marginTop: spacing.xl },
  exploreHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: spacing.md,
    minHeight: 48,
  },
  pressed: { opacity: 0.6 },
});

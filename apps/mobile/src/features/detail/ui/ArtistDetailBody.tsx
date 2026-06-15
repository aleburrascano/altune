import { useState, type ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native';

import { useRouter } from 'expo-router';

import { ChevronDown, ChevronRight } from 'lucide-react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { setDetailHandoff } from '@shared/lib/detail-handoff';
import { deriveAlbums } from '@shared/lib/derive-library-groups';

import { trackExtras } from '../extras-accessors';
import { useArtistContent } from '../hooks/useArtistContent';
import { useArtistDiscovery } from '../hooks/useArtistDiscovery';
import { useLibraryTracksForArtist, libraryTrackToDiscoveryResult } from '../hooks/useLibraryTracks';

import { sharedStyles } from './helpers';
import { DiscographySections } from './DiscographySections';

export function ArtistDetailBody({ result, detailRoute, isFromLibrary }: { result: DiscoveryResult; detailRoute: string; isFromLibrary?: boolean }): ReactElement {
  const router = useRouter();
  const theme = useTheme();
  const hasSources = !isFromLibrary && result.sources.length > 0;

  const localTracks = useLibraryTracksForArtist(result.title);
  const hasLibraryTracks = localTracks.length > 0;

  const [exploreExpanded, setExploreExpanded] = useState(hasSources);

  const discoverySearch = useArtistDiscovery({
    artistName: result.title,
    enabled: !hasSources && (exploreExpanded || hasLibraryTracks),
  });

  const effectiveSources = hasSources ? result.sources : (discoverySearch.sources.length > 0 ? discoverySearch.sources : result.sources);
  const effectiveMbid = hasSources
    ? trackExtras(result.extras).mbid
    : discoverySearch.mbid ?? trackExtras(result.extras).mbid;
  const shouldFetchContent = effectiveSources.length > 0 && exploreExpanded;

  const {
    topTracks: apiTopTracks,
    albums: apiAlbums,
    isLoadingTracks: apiLoadingTracks,
    isLoadingAlbums,
    isErrorTracks: apiErrorTracks,
    isErrorAlbums,
    refetchTracks,
    refetchAlbums,
  } = useArtistContent({
    sources: effectiveSources,
    mbid: effectiveMbid,
    enabled: shouldFetchContent,
  });

  const libraryTracksAsDiscovery = localTracks.map(libraryTrackToDiscoveryResult);
  const libraryAlbums: DiscoveryResult[] = deriveAlbums(localTracks).map((g) => ({
    kind: 'album' as const,
    title: g.album,
    subtitle: g.artist,
    image_url: g.artworkUrl,
    confidence: 'high' as const,
    sources: [],
    extras: {
      track_count: g.trackCount,
      ...(g.year != null ? { year: g.year } : {}),
    },
  }));

  const topTracks = hasSources ? apiTopTracks : libraryTracksAsDiscovery;
  const isLoadingTracks = hasSources ? apiLoadingTracks : false;
  const isErrorTracks = hasSources ? apiErrorTracks : false;

  const onTrackPress = (track: DiscoveryResult): void => {
    setDetailHandoff({
      ...track,
      image_url: track.image_url ?? result.image_url,
    });
    router.push(detailRoute as '/discover/detail');
  };

  const onAlbumPress = (album: DiscoveryResult): void => {
    setDetailHandoff(album);
    router.push(detailRoute as '/discover/detail');
  };

  return (
    <View testID="detail-artist-content" style={styles.artistContent}>
      {/* Your Tracks */}
      <Text variant="label" tone="secondary" style={sharedStyles.sectionTitle}>
        {hasSources ? 'Popular Tracks' : 'Your Tracks'}
      </Text>
      {isLoadingTracks ? (
        <View testID="detail-top-tracks-loading" style={styles.sectionLoading}>
          <ActivityIndicator />
        </View>
      ) : isErrorTracks ? (
        <View testID="detail-top-tracks-error" style={styles.sectionError}>
          <Text variant="body" tone="danger">
            Couldn't load tracks.
          </Text>
          <Button testID="detail-top-tracks-retry" label="Retry" onPress={() => refetchTracks()} style={sharedStyles.retryButton} />
        </View>
      ) : topTracks.length === 0 ? (
        <Text variant="body" tone="tertiary" style={styles.emptySection}>
          No tracks found.
        </Text>
      ) : (
        topTracks.map((track, index) => (
          <Pressable
            key={track.sources[0]?.external_id ?? index}
            testID={`detail-top-track-${index}`}
            onPress={() => onTrackPress(track)}
            accessibilityRole="button"
            accessibilityLabel={`Play ${track.title}`}
            style={({ pressed }) => [sharedStyles.trackRow, pressed ? { opacity: 0.6 } : null]}
          >
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
          </Pressable>
        ))
      )}

      {/* Library Albums (when from library) */}
      {!hasSources && libraryAlbums.length > 0 ? (
        <View style={sharedStyles.albumsSection}>
          <Text variant="label" tone="secondary" style={sharedStyles.sectionTitle}>
            Your Albums
          </Text>
          <DiscographySections albums={libraryAlbums} onAlbumPress={onAlbumPress} />
        </View>
      ) : null}

      {/* Discovery Discography — always shown when hasSources, expandable when from library */}
      {hasSources ? (
        <>
          {isLoadingAlbums ? (
            <View testID="detail-albums-loading" style={[styles.sectionLoading, sharedStyles.albumsSection]}>
              <ActivityIndicator />
            </View>
          ) : isErrorAlbums ? (
            <View testID="detail-albums-error" style={[styles.sectionError, sharedStyles.albumsSection]}>
              <Text variant="body" tone="danger">
                Couldn't load albums.
              </Text>
              <Button testID="detail-albums-retry" label="Retry" onPress={() => refetchAlbums()} style={sharedStyles.retryButton} />
            </View>
          ) : apiAlbums.length === 0 ? (
            <Text variant="body" tone="tertiary" style={[styles.emptySection, sharedStyles.albumsSection]}>
              No albums found.
            </Text>
          ) : (
            <DiscographySections albums={apiAlbums} onAlbumPress={onAlbumPress} />
          )}
        </>
      ) : (
        <View style={styles.exploreSection}>
          <View style={[styles.separator, { backgroundColor: theme.color.border }]} />
          <Pressable
            testID="detail-explore-discography"
            onPress={() => setExploreExpanded((prev) => !prev)}
            accessibilityRole="button"
            accessibilityLabel={exploreExpanded ? 'Collapse discography' : 'Explore full discography'}
            style={({ pressed }) => [styles.exploreHeader, pressed ? { opacity: 0.6 } : null]}
          >
            <Text variant="label" tone="accent">
              Explore Discography
            </Text>
            {exploreExpanded ? (
              <ChevronDown size={18} color={theme.color.accent} />
            ) : (
              <ChevronRight size={18} color={theme.color.accent} />
            )}
          </Pressable>

          {exploreExpanded ? (
            discoverySearch.isLoading || isLoadingAlbums ? (
              <View style={styles.sectionLoading}>
                <ActivityIndicator />
              </View>
            ) : discoverySearch.isError || isErrorAlbums ? (
              <View style={styles.sectionError}>
                <Text variant="caption" tone="secondary">
                  Couldn't load discography.
                </Text>
                <Button label="Retry" onPress={() => refetchAlbums()} style={sharedStyles.retryButton} />
              </View>
            ) : apiAlbums.length === 0 ? (
              <Text variant="caption" tone="tertiary" style={styles.emptySection}>
                No additional albums found.
              </Text>
            ) : (
              <DiscographySections albums={apiAlbums} onAlbumPress={onAlbumPress} />
            )
          ) : null}
        </View>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  artistContent: { marginTop: spacing.lg },
  sectionLoading: { paddingVertical: spacing.lg, alignItems: 'center' },
  sectionError: { paddingVertical: spacing.md, alignItems: 'center' },
  emptySection: { paddingVertical: spacing.md },
  exploreSection: { marginTop: spacing.xl },
  separator: { height: StyleSheet.hairlineWidth, marginBottom: spacing.lg },
  exploreHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: spacing.md,
  },
});

import type { ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native';

import { useRouter } from 'expo-router';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { setDetailHandoff } from '@shared/lib/detail-handoff';

import { useArtistContent } from '../hooks/useArtistContent';

import { sharedStyles } from './helpers';
import { DiscographySections } from './DiscographySections';

/** Artist body: top tracks + albums fetched from provider API. */
export function ArtistDetailBody({ result, detailRoute }: { result: DiscoveryResult; detailRoute: string }): ReactElement {
  const router = useRouter();
  const {
    topTracks,
    albums,
    isLoadingTracks,
    isLoadingAlbums,
    isErrorTracks,
    isErrorAlbums,
    refetchTracks,
    refetchAlbums,
  } = useArtistContent({
    sources: result.sources,
    mbid: typeof result.extras['mbid'] === 'string' ? result.extras['mbid'] : null,
    enabled: result.sources.length > 0,
  });

  const onTrackPress = (track: DiscoveryResult): void => {
    const enriched = {
      ...track,
      image_url: track.image_url ?? result.image_url,
    };
    setDetailHandoff(enriched);
    router.push(detailRoute as '/discover/detail');
  };

  const onAlbumPress = (album: DiscoveryResult): void => {
    setDetailHandoff(album);
    router.push(detailRoute as '/discover/detail');
  };

  return (
    <View testID="detail-artist-content" style={styles.artistContent}>
      {/* Popular Tracks Section */}
      <Text variant="label" tone="secondary" style={sharedStyles.sectionTitle}>
        Popular Tracks
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

      {/* Discography Sections — grouped by record_type */}
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
      ) : albums.length === 0 ? (
        <Text variant="body" tone="tertiary" style={[styles.emptySection, sharedStyles.albumsSection]}>
          No albums found.
        </Text>
      ) : (
        <DiscographySections albums={albums} onAlbumPress={onAlbumPress} />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  artistContent: { marginTop: spacing.lg },
  sectionLoading: { paddingVertical: spacing.lg, alignItems: 'center' },
  sectionError: { paddingVertical: spacing.md, alignItems: 'center' },
  emptySection: { paddingVertical: spacing.md },
});

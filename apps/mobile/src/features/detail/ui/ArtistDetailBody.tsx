import { useState, type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { ChevronDown, ChevronRight } from 'lucide-react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { formatDuration } from '@shared/lib/format';

import { trackExtras } from '../extras-accessors';
import { useArtistDetailState } from '../hooks/useArtistDetailState';
import type { DetailRoute } from '../navigation';

import { sharedStyles } from './helpers';
import { AlbumCardsSkeleton, TrackRowsSkeleton } from './DetailSkeleton';
import { DiscographySections } from './DiscographySections';
import { TrackSaveControl } from './TrackSaveControl';

// Your library can hold dozens of tracks by one artist; showing them all buries
// the discography below an endless scroll. Cap the collapsed view and reveal the
// rest on demand. (Discovery "Popular Tracks" is already API-limited, so the cap
// only bites the library "Your Tracks" list.)
const TRACK_CAP = 5;

export function ArtistDetailBody({ result, detailRoute, isFromLibrary }: { result: DiscoveryResult; detailRoute: DetailRoute; isFromLibrary?: boolean }): ReactElement {
  const theme = useTheme();
  const artist = useArtistDetailState(result, detailRoute, isFromLibrary);
  const [showAllTracks, setShowAllTracks] = useState(false);
  const visibleTracks =
    artist.hasSources || showAllTracks ? artist.topTracks : artist.topTracks.slice(0, TRACK_CAP);

  return (
    <View testID="detail-artist-content" style={styles.artistContent}>
      {/* Your Tracks */}
      <Text variant="label" tone="secondary" style={sharedStyles.sectionTitle}>
        {artist.hasSources ? 'Popular Tracks' : 'Your Tracks'}
      </Text>
      {artist.isLoadingTracks ? (
        <TrackRowsSkeleton testID="detail-top-tracks-loading" count={5} />
      ) : artist.isErrorTracks ? (
        <View testID="detail-top-tracks-error" style={styles.sectionError}>
          <Text variant="body" tone="danger">
            Couldn't load tracks.
          </Text>
          <Button testID="detail-top-tracks-retry" label="Retry" onPress={() => artist.refetchTracks()} style={sharedStyles.retryButton} />
        </View>
      ) : artist.topTracks.length === 0 ? (
        <Text variant="body" tone="tertiary" style={styles.emptySection}>
          No tracks found.
        </Text>
      ) : (
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
                {durationSeconds != null ? (
                  <Text variant="label" tone="tertiary" style={styles.trackDuration}>
                    {formatDuration(durationSeconds)}
                  </Text>
                ) : null}
                <TrackSaveControl
                  testID={`detail-top-track-save-${index}`}
                  state={artist.saveStateFor(track.title, track.subtitle)}
                  title={track.title}
                  onPress={() => artist.onQuickSave(track)}
                />
              </Pressable>
            );
          })}
          {!artist.hasSources && !showAllTracks && artist.topTracks.length > TRACK_CAP ? (
            <Pressable
              testID="detail-show-all-tracks"
              onPress={() => setShowAllTracks(true)}
              accessibilityRole="button"
              accessibilityLabel={`Show all ${artist.topTracks.length} tracks`}
              style={({ pressed }) => [styles.showAll, pressed ? { opacity: 0.6 } : null]}
            >
              <Text variant="label" tone="accent">
                Show all {artist.topTracks.length} tracks
              </Text>
            </Pressable>
          ) : null}
        </>
      )}

      {/* Library Albums (when from library) */}
      {!artist.hasSources && artist.libraryAlbums.length > 0 ? (
        <View style={sharedStyles.albumsSection}>
          <Text variant="label" tone="secondary" style={sharedStyles.sectionTitle}>
            Your Albums
          </Text>
          <DiscographySections albums={artist.libraryAlbums} onAlbumPress={artist.onAlbumPress} />
        </View>
      ) : null}

      {/* Discovery Discography — always shown when hasSources, expandable when from library */}
      {artist.hasSources ? (
        <>
          {artist.isLoadingAlbums ? (
            <View testID="detail-albums-loading" style={sharedStyles.albumsSection}>
              <AlbumCardsSkeleton />
            </View>
          ) : artist.isErrorAlbums ? (
            <View testID="detail-albums-error" style={[styles.sectionError, sharedStyles.albumsSection]}>
              <Text variant="body" tone="danger">
                Couldn't load albums.
              </Text>
              <Button testID="detail-albums-retry" label="Retry" onPress={() => artist.refetchAlbums()} style={sharedStyles.retryButton} />
            </View>
          ) : artist.apiAlbums.length === 0 ? (
            <Text variant="body" tone="tertiary" style={[styles.emptySection, sharedStyles.albumsSection]}>
              No albums found.
            </Text>
          ) : (
            <DiscographySections albums={artist.apiAlbums} onAlbumPress={artist.onAlbumPress} />
          )}
        </>
      ) : (
        <View style={styles.exploreSection}>
          <View style={[styles.separator, { backgroundColor: theme.color.border }]} />
          <Pressable
            testID="detail-explore-discography"
            onPress={() => artist.setExploreExpanded((prev) => !prev)}
            accessibilityRole="button"
            accessibilityLabel={artist.exploreExpanded ? 'Collapse discography' : 'Explore full discography'}
            style={({ pressed }) => [styles.exploreHeader, pressed ? { opacity: 0.6 } : null]}
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

          {artist.exploreExpanded ? (
            artist.discoveryLoading || artist.isLoadingAlbums ? (
              <AlbumCardsSkeleton />
            ) : artist.discoveryError || artist.isErrorAlbums ? (
              <View style={styles.sectionError}>
                <Text variant="caption" tone="secondary">
                  Couldn't load discography.
                </Text>
                <Button label="Retry" onPress={() => artist.refetchAlbums()} style={sharedStyles.retryButton} />
              </View>
            ) : artist.apiAlbums.length === 0 ? (
              <Text variant="caption" tone="tertiary" style={styles.emptySection}>
                No additional albums found.
              </Text>
            ) : (
              <DiscographySections albums={artist.apiAlbums} onAlbumPress={artist.onAlbumPress} />
            )
          ) : null}
        </View>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  artistContent: { marginTop: spacing.lg },
  trackDuration: { marginRight: spacing.xs, fontVariant: ['tabular-nums'] },
  showAll: { paddingVertical: spacing.md, alignItems: 'center' },
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

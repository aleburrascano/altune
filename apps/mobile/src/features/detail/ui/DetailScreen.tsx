/**
 * DetailScreen — read-only detail for a tapped discovery result.
 *
 * Fed by the in-memory handoff (no per-item backend fetch). The header (back
 * affordance + hero artwork + title/subtitle/kind) is shared across kinds;
 * the body differs per kind (track info rows + Save; album/artist placeholders)
 * and is filled in by later slices. An empty handoff redirects to /discover.
 *
 * Primitives are imported directly (not via the @shared/ui barrel) so jest
 * component tests don't transitively load unrelated native modules; Artwork's
 * expo-image dependency is mocked in the test.
 */

import { Redirect, useRouter } from 'expo-router';
import type { ReactElement } from 'react';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, View } from 'react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Screen } from '@shared/ui/primitives/Screen';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme/tokens';

import { getDetailHandoff, setDetailHandoff } from '@shared/lib/detail-handoff';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { formatDuration, trackInfoRows } from '../extras';
import { useAlbumTracks } from '../hooks/useAlbumTracks';
import { useArtistContent } from '../hooks/useArtistContent';
import { useLateralNav } from '../hooks/useLateralNav';
import { useSaveTrack } from '../hooks/useSaveTrack';
import { toCreateTrackRequest } from '../save-cache';

const HERO_SIZE = 200;

function _kindLabel(kind: 'artist' | 'album' | 'track'): string {
  if (kind === 'artist') {
    return 'Artist';
  }
  return kind === 'album' ? 'Album' : 'Song';
}

export function DetailScreen(): ReactElement {
  const router = useRouter();
  const result = getDetailHandoff();
  const lateralNav = useLateralNav();

  if (result === null) {
    return <Redirect href="/discover" />;
  }

  const isArtist = result.kind === 'artist';
  const canNavToArtist = result.subtitle !== null && result.kind !== 'artist';

  const onArtistPress = (): void => {
    if (canNavToArtist && result.subtitle !== null) {
      void lateralNav.navigateTo(result.subtitle, 'artist');
    }
  };

  const heroContent = (
    <>
      <Pressable
        testID="detail-back"
        onPress={() => router.back()}
        style={({ pressed }) => [styles.back, pressed ? { opacity: 0.6 } : null]}
      >
        <Text variant="label" tone="accent">
          ‹ Back
        </Text>
      </Pressable>

      <View style={styles.hero}>
        <Artwork
          uri={result.image_url}
          size={HERO_SIZE}
          radius={isArtist ? radius.full : radius.lg}
          accessibilityLabel={result.title}
        />
        <Text variant="displayL" style={styles.title} numberOfLines={2}>
          {result.title}
        </Text>
        {result.subtitle !== null ? (
          canNavToArtist ? (
            <Pressable
              testID="detail-artist-link"
              onPress={onArtistPress}
              disabled={lateralNav.state === 'searching'}
              style={({ pressed }) => (pressed ? { opacity: 0.6 } : null)}
            >
              <Text variant="body" tone="accent" numberOfLines={1}>
                {result.subtitle}
              </Text>
            </Pressable>
          ) : (
            <Text variant="body" tone="secondary" numberOfLines={1}>
              {result.subtitle}
            </Text>
          )
        ) : null}
        <Text variant="label" tone="tertiary" style={styles.kind}>
          {_kindLabel(result.kind)}
        </Text>
      </View>
    </>
  );

  // Track detail: no scroll needed (content is short)
  if (result.kind === 'track') {
    return (
      <Screen testID="detail-header">
        {heroContent}
        <TrackDetailBody result={result} lateralNav={lateralNav} />
      </Screen>
    );
  }

  // Album/Artist detail: single scroll for hero + content
  return (
    <Screen testID="detail-header">
      <ScrollView
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.scrollContent}
      >
        {heroContent}
        {result.kind === 'album' ? <AlbumDetailBody result={result} /> : null}
        {result.kind === 'artist' ? <ArtistDetailBody result={result} /> : null}
      </ScrollView>
    </Screen>
  );
}

type LateralNavHandle = {
  navigateTo: (query: string, kind: 'artist' | 'album' | 'track') => Promise<void>;
  state: 'idle' | 'searching';
};

/** Track body: info rows + an optimistic Save-to-library action. */
function TrackDetailBody({
  result,
  lateralNav,
}: {
  result: DiscoveryResult;
  lateralNav: LateralNavHandle;
}): ReactElement {
  const save = useSaveTrack();
  const rows = trackInfoRows(result.extras);
  // AC#9: a Track requires a non-empty artist. When the result has no subtitle
  // (artist), that invariant can't be met — disable Save rather than POST an
  // invalid body.
  const canSave = result.subtitle !== null && result.subtitle.length > 0;

  const onSave = (): void => {
    if (!canSave) {
      return;
    }
    save.mutate(toCreateTrackRequest(result));
  };

  const albumName =
    typeof result.extras['album'] === 'string' && result.extras['album'].length > 0
      ? result.extras['album']
      : null;

  const onAlbumPress = (): void => {
    if (albumName !== null && result.subtitle !== null) {
      // Include artist for disambiguation: "Album Name Artist Name"
      void lateralNav.navigateTo(`${albumName} ${result.subtitle}`, 'album');
    }
  };

  return (
    <View testID="detail-track-info" style={styles.info}>
      {rows.map((row) =>
        row.key === 'album' && albumName !== null ? (
          <Pressable
            key={row.key}
            testID="detail-info-album"
            onPress={onAlbumPress}
            disabled={lateralNav.state === 'searching'}
            style={({ pressed }) => [styles.infoRow, pressed ? { opacity: 0.6 } : null]}
          >
            <Text variant="label" tone="tertiary">
              {row.label}
            </Text>
            <Text variant="body" tone="accent">
              {row.value}
            </Text>
          </Pressable>
        ) : (
          <View key={row.key} testID={`detail-info-${row.key}`} style={styles.infoRow}>
            <Text variant="label" tone="tertiary">
              {row.label}
            </Text>
            <Text variant="body">{row.value}</Text>
          </View>
        ),
      )}
      <Button
        testID="detail-save"
        label={save.isSuccess ? 'Saved' : 'Save to Library'}
        onPress={onSave}
        disabled={!canSave || save.isPending || save.isSuccess}
        loading={save.isPending}
        haptic
        style={styles.save}
      />
      {save.isError ? (
        <Banner testID="detail-save-error" tone="danger">
          Couldn’t save this track. Try again.
        </Banner>
      ) : null}
    </View>
  );
}

/** Album body: track list fetched from provider API. */
function AlbumDetailBody({ result }: { result: DiscoveryResult }): ReactElement {
  const router = useRouter();
  const source = result.sources[0];
  const { tracks, isLoading, isError, refetch } = useAlbumTracks({
    provider: source?.provider ?? '',
    externalId: source?.external_id ?? '',
    enabled: source !== undefined,
  });

  const onTrackPress = (track: DiscoveryResult): void => {
    setDetailHandoff(track);
    router.replace('/detail');
  };

  if (isLoading) {
    return (
      <View testID="detail-tracklist-loading" style={styles.placeholder}>
        <ActivityIndicator />
      </View>
    );
  }

  if (isError) {
    return (
      <View testID="detail-tracklist-error" style={styles.placeholder}>
        <Text variant="body" tone="danger">
          Couldn't load tracks.
        </Text>
        <Button label="Retry" onPress={() => refetch()} style={styles.retryButton} />
      </View>
    );
  }

  if (tracks.length === 0) {
    return (
      <View testID="detail-tracklist-empty" style={styles.placeholder}>
        <Text variant="body" tone="tertiary">
          No tracks found.
        </Text>
      </View>
    );
  }

  return (
    <View testID="detail-tracklist" style={styles.trackList}>
      {tracks.map((track, index) => {
        const position =
          typeof track.extras['track_position'] === 'number'
            ? track.extras['track_position']
            : index + 1;
        const duration =
          typeof track.extras['duration_seconds'] === 'number'
            ? formatDuration(track.extras['duration_seconds'])
            : null;
        return (
          <Pressable
            key={track.sources[0]?.external_id ?? index}
            testID={`detail-track-${index}`}
            onPress={() => onTrackPress(track)}
            style={({ pressed }) => [styles.trackRow, pressed ? { opacity: 0.6 } : null]}
          >
            <Text variant="label" tone="tertiary" style={styles.trackPosition}>
              {position}
            </Text>
            <View style={styles.trackInfo}>
              <Text variant="body" numberOfLines={1}>
                {track.title}
              </Text>
              {track.subtitle ? (
                <Text variant="label" tone="secondary" numberOfLines={1}>
                  {track.subtitle}
                </Text>
              ) : null}
            </View>
            {duration ? (
              <Text variant="label" tone="tertiary" style={styles.trackDuration}>
                {duration}
              </Text>
            ) : null}
          </Pressable>
        );
      })}
    </View>
  );
}

/** Artist body: top tracks + albums fetched from provider API. */
function ArtistDetailBody({ result }: { result: DiscoveryResult }): ReactElement {
  const router = useRouter();
  const source = result.sources[0];
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
    provider: source?.provider ?? '',
    externalId: source?.external_id ?? '',
    enabled: source !== undefined,
  });

  const onTrackPress = (track: DiscoveryResult): void => {
    setDetailHandoff(track);
    router.replace('/detail');
  };

  const onAlbumPress = (album: DiscoveryResult): void => {
    setDetailHandoff(album);
    router.replace('/detail');
  };

  return (
    <View testID="detail-artist-content" style={styles.artistContent}>
      {/* Popular Tracks Section */}
      <Text variant="label" tone="secondary" style={styles.sectionTitle}>
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
          <Button label="Retry" onPress={() => refetchTracks()} style={styles.retryButton} />
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
            style={({ pressed }) => [styles.trackRow, pressed ? { opacity: 0.6 } : null]}
          >
            <Artwork
              uri={track.image_url}
              size={40}
              radius={radius.sm}
              accessibilityLabel={track.title}
            />
            <View style={styles.trackInfo}>
              <Text variant="body" numberOfLines={1}>
                {track.title}
              </Text>
            </View>
          </Pressable>
        ))
      )}

      {/* Albums Section */}
      <Text variant="label" tone="secondary" style={[styles.sectionTitle, styles.albumsSection]}>
        Albums
      </Text>
      {isLoadingAlbums ? (
        <View testID="detail-albums-loading" style={styles.sectionLoading}>
          <ActivityIndicator />
        </View>
      ) : isErrorAlbums ? (
        <View testID="detail-albums-error" style={styles.sectionError}>
          <Text variant="body" tone="danger">
            Couldn't load albums.
          </Text>
          <Button label="Retry" onPress={() => refetchAlbums()} style={styles.retryButton} />
        </View>
      ) : albums.length === 0 ? (
        <Text variant="body" tone="tertiary" style={styles.emptySection}>
          No albums found.
        </Text>
      ) : (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.albumsScroll}>
          {albums.map((album, index) => (
            <Pressable
              key={album.sources[0]?.external_id ?? index}
              testID={`detail-album-${index}`}
              onPress={() => onAlbumPress(album)}
              style={({ pressed }) => [styles.albumCard, pressed ? { opacity: 0.6 } : null]}
            >
              <Artwork
                uri={album.image_url}
                size={120}
                radius={radius.md}
                accessibilityLabel={album.title}
              />
              <Text variant="label" numberOfLines={2} style={styles.albumTitle}>
                {album.title}
              </Text>
            </Pressable>
          ))}
        </ScrollView>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  scrollContent: { paddingBottom: spacing['2xl'] },
  back: { paddingVertical: spacing.md, alignSelf: 'flex-start' },
  hero: { alignItems: 'center', paddingTop: spacing.lg, gap: spacing.sm },
  title: { textAlign: 'center', marginTop: spacing.lg },
  kind: { marginTop: spacing.xs },
  info: { marginTop: spacing['2xl'], gap: spacing.md },
  infoRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    gap: spacing.lg,
  },
  placeholder: { marginTop: spacing['2xl'], alignItems: 'center' },
  save: { marginTop: spacing.lg },
  // Album tracklist styles
  trackList: { marginTop: spacing.lg },
  trackRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: spacing.sm,
    gap: spacing.md,
  },
  trackPosition: { width: 24, textAlign: 'center' },
  trackInfo: { flex: 1 },
  trackDuration: { marginRight: spacing.sm },
  retryButton: { marginTop: spacing.sm },
  // Artist content styles
  artistContent: { marginTop: spacing.lg },
  sectionTitle: { marginBottom: spacing.sm },
  sectionLoading: { paddingVertical: spacing.lg, alignItems: 'center' },
  sectionError: { paddingVertical: spacing.md, alignItems: 'center' },
  emptySection: { paddingVertical: spacing.md },
  albumsSection: { marginTop: spacing.xl },
  albumsScroll: { marginHorizontal: -spacing.lg },
  albumCard: { width: 120, marginLeft: spacing.lg },
  albumTitle: { marginTop: spacing.xs, textAlign: 'center' },
});

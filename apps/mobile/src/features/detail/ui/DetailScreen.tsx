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

import { Redirect, useRouter, useSegments } from 'expo-router';
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

import { useQueryClient } from '@tanstack/react-query';
import type { InfiniteData } from '@tanstack/react-query';
import type { ListTracksResponse } from '@shared/api-client/types';

import { extractFeaturedFromText, formatDuration, trackInfoRows } from '../extras';
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
  const segments = useSegments();
  const tabRoot = segments[1] === 'library' ? 'library' : 'discover';
  const detailRoute = `/${tabRoot}/detail` as const;
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

  const backButton = (
    <Pressable
      testID="detail-back"
      onPress={() => {
        if (router.canGoBack()) {
          router.back();
        } else {
          router.replace(`/${tabRoot}` as '/discover' | '/library');
        }
      }}
      accessibilityRole="button"
      accessibilityLabel="Go back"
      style={({ pressed }) => [styles.back, pressed ? { opacity: 0.6 } : null]}
    >
      <Text variant="label" tone="accent">
        ‹ Back
      </Text>
    </Pressable>
  );

  const heroContent = (
    <>
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
              accessibilityRole="link"
              accessibilityLabel={`View artist ${result.subtitle}`}
              accessibilityHint="Opens artist detail"
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
        {backButton}
        {heroContent}
        <TrackDetailBody result={result} lateralNav={lateralNav} />
      </Screen>
    );
  }

  // Album/Artist detail: single scroll for hero + content, sticky back button
  return (
    <Screen testID="detail-header">
      {backButton}
      <ScrollView
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.scrollContent}
      >
        {heroContent}
        {result.kind === 'album' ? <AlbumDetailBody result={result} detailRoute={detailRoute} /> : null}
        {result.kind === 'artist' ? <ArtistDetailBody result={result} detailRoute={detailRoute} /> : null}
      </ScrollView>
    </Screen>
  );
}

type LateralNavHandle = {
  navigateTo: (query: string, kind: 'artist' | 'album' | 'track') => Promise<void>;
  state: 'idle' | 'searching';
  error: string | null;
  clearError: () => void;
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
  const queryClient = useQueryClient();
  const alreadySaved = _isTrackInLibraryCache(queryClient, result.title, result.subtitle);
  const rows = trackInfoRows(result.extras);
  if (!rows.some((r) => r.key === 'featuring')) {
    const parsed = extractFeaturedFromText(result.title, result.subtitle);
    if (parsed) {
      rows.push({ key: 'featuring', label: 'Featuring', value: parsed });
    }
  }
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
            accessibilityRole="link"
            accessibilityLabel={`View album ${row.value}`}
            accessibilityHint="Opens album detail"
            style={({ pressed }) => [styles.infoRow, pressed ? { opacity: 0.6 } : null]}
          >
            <Text variant="label" tone="tertiary">
              {row.label}
            </Text>
            <Text variant="body" tone="accent">
              {row.value}
            </Text>
          </Pressable>
        ) : row.key === 'featuring' ? (
          <View key={row.key} testID="detail-info-featuring" style={styles.infoRow}>
            <Text variant="label" tone="tertiary">
              {row.label}
            </Text>
            <View style={styles.featuredArtists}>
              {row.value.split(', ').map((name, i) => (
                <Pressable
                  key={name}
                  onPress={() => void lateralNav.navigateTo(name, 'artist')}
                  disabled={lateralNav.state === 'searching'}
                  accessibilityRole="link"
                  accessibilityLabel={`View artist ${name}`}
                >
                  {({ pressed }) => (
                    <Text variant="body" tone="accent" style={pressed ? { opacity: 0.6 } : undefined}>
                      {i > 0 ? `, ${name}` : name}
                    </Text>
                  )}
                </Pressable>
              ))}
            </View>
          </View>
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
        label={alreadySaved || save.isSuccess ? 'Saved' : 'Save to Library'}
        onPress={onSave}
        disabled={!canSave || save.isPending || save.isSuccess || alreadySaved}
        loading={save.isPending}
        haptic
        style={styles.save}
      />
      {save.isError ? (
        <Banner testID="detail-save-error" tone="danger">
          Couldn't save this track. Tap Save to retry.
        </Banner>
      ) : null}
      {lateralNav.error !== null ? (
        <Banner testID="detail-lateral-error" tone="danger">
          {lateralNav.error}
        </Banner>
      ) : null}
      {lateralNav.state === 'searching' ? (
        <View style={styles.lateralLoading}>
          <ActivityIndicator size="small" />
          <Text variant="label" tone="secondary">
            Searching...
          </Text>
        </View>
      ) : null}
    </View>
  );
}

function _trackSubtitleWithFeaturing(track: DiscoveryResult): string {
  const base = track.subtitle ?? '';
  const mbFeat = track.extras['featured_artists'];
  if (Array.isArray(mbFeat) && mbFeat.length > 0) {
    const names = mbFeat.filter((n): n is string => typeof n === 'string' && n.length > 0);
    if (names.length > 0) return `${base}, ${names.join(', ')}`;
  }
  const parsed = extractFeaturedFromText(track.title, track.subtitle);
  if (parsed) return `${base}, ${parsed}`;
  return base;
}

/** Album body: track list fetched from provider API. */
function AlbumDetailBody({ result, detailRoute }: { result: DiscoveryResult; detailRoute: string }): ReactElement {
  const router = useRouter();
  const source = result.sources[0];
  const { tracks, isLoading, isError, refetch } = useAlbumTracks({
    provider: source?.provider ?? '',
    externalId: source?.external_id ?? '',
    allSources: result.sources,
    enabled: source !== undefined,
  });

  const onTrackPress = (track: DiscoveryResult): void => {
    const enriched = {
      ...track,
      image_url: track.image_url ?? result.image_url,
      extras: {
        ...track.extras,
        album: track.extras['album'] ?? result.title,
        album_artist: track.extras['album_artist'] ?? result.subtitle,
      },
    };
    setDetailHandoff(enriched);
    router.push(detailRoute as '/discover/detail');
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
        <Button testID="detail-tracklist-retry" label="Retry" onPress={() => refetch()} style={styles.retryButton} />
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

  const albumYear = _albumYear(result);
  const totalDurationSec = tracks.reduce((sum, t) => {
    const dur = t.extras['duration_seconds'];
    return sum + (typeof dur === 'number' ? dur : 0);
  }, 0);
  const metaParts: string[] = [];
  if (albumYear) metaParts.push(albumYear);
  metaParts.push(`${tracks.length} track${tracks.length !== 1 ? 's' : ''}`);
  if (totalDurationSec > 0) {
    const totalMin = Math.floor(totalDurationSec / 60);
    const hrs = Math.floor(totalMin / 60);
    const mins = totalMin % 60;
    metaParts.push(hrs > 0 ? `${hrs} hr ${mins} min` : `${mins} min`);
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
        const a11yLabel = `Track ${position}: ${track.title}${duration ? `, ${duration}` : ''}`;
        return (
          <Pressable
            key={track.sources[0]?.external_id ?? index}
            testID={`detail-track-${index}`}
            onPress={() => onTrackPress(track)}
            accessibilityRole="button"
            accessibilityLabel={a11yLabel}
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
                  {_trackSubtitleWithFeaturing(track)}
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
      <Text testID="detail-album-meta" variant="label" tone="tertiary" style={styles.albumMeta}>
        {metaParts.join(' · ')}
      </Text>
    </View>
  );
}

const DISCOGRAPHY_SECTIONS: ReadonlyArray<{ type: string; label: string }> = [
  { type: 'album', label: 'Albums' },
  { type: 'single', label: 'Singles' },
  { type: 'ep', label: 'EPs' },
];

function _albumYear(album: DiscoveryResult): string | null {
  const releaseDate = album.extras['release_date'];
  if (typeof releaseDate === 'string') return releaseDate.slice(0, 4);
  const yearExtra = album.extras['year'];
  if (typeof yearExtra === 'string' || typeof yearExtra === 'number') return String(yearExtra);
  return null;
}

function DiscographySections({
  albums,
  onAlbumPress,
}: {
  albums: DiscoveryResult[];
  onAlbumPress: (album: DiscoveryResult) => void;
}): ReactElement {
  const grouped = new Map<string, DiscoveryResult[]>();
  for (const album of albums) {
    const rawType = album.extras['record_type'];
    const type = typeof rawType === 'string' ? rawType.toLowerCase() : 'album';
    const bucket = type === 'compilation' ? 'album' : type;
    const list = grouped.get(bucket);
    if (list) {
      list.push(album);
    } else {
      grouped.set(bucket, [album]);
    }
  }

  return (
    <>
      {DISCOGRAPHY_SECTIONS.map((section) => {
        const items = grouped.get(section.type);
        if (!items || items.length === 0) return null;
        return (
          <View key={section.type} style={styles.albumsSection}>
            <Text variant="label" tone="secondary" style={styles.sectionTitle}>
              {section.label}
            </Text>
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              style={styles.albumsScroll}
              contentContainerStyle={styles.albumsScrollContent}
            >
              {items.map((album, index) => {
                const year = _albumYear(album);
                const trackCount = typeof album.extras['track_count'] === 'number'
                  ? album.extras['track_count']
                  : null;
                return (
                  <Pressable
                    key={album.sources[0]?.external_id ?? index}
                    testID={`detail-${section.type}-${index}`}
                    onPress={() => onAlbumPress(album)}
                    accessibilityRole="button"
                    accessibilityLabel={`${section.label}: ${album.title}${year ? `, ${year}` : ''}${trackCount ? `, ${trackCount} tracks` : ''}`}
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
                    {year ? (
                      <Text variant="caption" tone="tertiary">
                        {year}
                      </Text>
                    ) : null}
                    {trackCount !== null ? (
                      <Text variant="caption" tone="tertiary">
                        {trackCount} tracks
                      </Text>
                    ) : null}
                  </Pressable>
                );
              })}
            </ScrollView>
          </View>
        );
      })}
    </>
  );
}

/** Artist body: top tracks + albums fetched from provider API. */
function ArtistDetailBody({ result, detailRoute }: { result: DiscoveryResult; detailRoute: string }): ReactElement {
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
    artistName: result.title,
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
          <Button testID="detail-top-tracks-retry" label="Retry" onPress={() => refetchTracks()} style={styles.retryButton} />
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

      {/* Discography Sections — grouped by record_type */}
      {isLoadingAlbums ? (
        <View testID="detail-albums-loading" style={[styles.sectionLoading, styles.albumsSection]}>
          <ActivityIndicator />
        </View>
      ) : isErrorAlbums ? (
        <View testID="detail-albums-error" style={[styles.sectionError, styles.albumsSection]}>
          <Text variant="body" tone="danger">
            Couldn't load albums.
          </Text>
          <Button testID="detail-albums-retry" label="Retry" onPress={() => refetchAlbums()} style={styles.retryButton} />
        </View>
      ) : albums.length === 0 ? (
        <Text variant="body" tone="tertiary" style={[styles.emptySection, styles.albumsSection]}>
          No albums found.
        </Text>
      ) : (
        <DiscographySections albums={albums} onAlbumPress={onAlbumPress} />
      )}
    </View>
  );
}

function _isTrackInLibraryCache(
  queryClient: ReturnType<typeof useQueryClient>,
  title: string,
  artist: string | null,
): boolean {
  const data = queryClient.getQueryData<InfiniteData<ListTracksResponse>>(['library']);
  if (!data) return false;
  const normalTitle = title.toLowerCase().trim();
  const normalArtist = (artist ?? '').toLowerCase().trim();
  return data.pages.some((page) =>
    page.items.some(
      (t) => t.title.toLowerCase().trim() === normalTitle && t.artist.toLowerCase().trim() === normalArtist,
    ),
  );
}

const styles = StyleSheet.create({
  scrollContent: { paddingBottom: spacing['2xl'] },
  back: { paddingVertical: spacing.lg, alignSelf: 'flex-start', minHeight: 48 },
  hero: { alignItems: 'center', paddingTop: spacing.lg, gap: spacing.sm },
  title: { textAlign: 'center', marginTop: spacing.lg },
  kind: { marginTop: spacing.xs },
  info: { marginTop: spacing['2xl'], gap: spacing.md },
  infoRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    gap: spacing.lg,
    minHeight: 48,
  },
  featuredArtists: { flexDirection: 'row', flexWrap: 'wrap' },
  placeholder: { marginTop: spacing['2xl'], alignItems: 'center' },
  save: { marginTop: spacing.lg },
  // Album tracklist styles
  trackList: { marginTop: spacing.lg },
  albumMeta: { marginTop: spacing.lg, textAlign: 'center' as const },
  trackRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: spacing.md,
    gap: spacing.md,
    minHeight: 48,
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
  albumsScrollContent: { paddingRight: spacing.lg },
  albumCard: { width: 120, marginLeft: spacing.lg },
  albumTitle: { marginTop: spacing.xs },
  lateralLoading: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.sm,
    marginTop: spacing.md,
  },
});

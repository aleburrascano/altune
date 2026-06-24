import { useState, type ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native';

import { useRouter } from 'expo-router';

import { useQueryClient } from '@tanstack/react-query';
import { ChevronDown, ChevronRight } from 'lucide-react-native';

import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';
import { spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { setDetailHandoff } from '@shared/lib/detail-handoff';

import { extractFeaturedFromText } from '../extras';
import { trackExtras } from '../extras-accessors';
import { useAlbumDiscovery } from '../hooks/useAlbumDiscovery';
import { useAlbumTracks } from '../hooks/useAlbumTracks';
import { useLibraryTracksForAlbum, libraryTrackToDiscoveryResult } from '../hooks/useLibraryTracks';
import { useSaveTrack } from '../hooks/useSaveTrack';
import { toCreateTrackRequest } from '../save-cache';

import { _albumYear, _isTrackInLibraryCache, sharedStyles } from './helpers';
import { AlbumTrackRow } from './AlbumTrackRow';

function _trackSubtitleWithFeaturing(track: DiscoveryResult): string {
  const base = track.subtitle ?? '';
  const names = trackExtras(track.extras).featuredArtists;
  if (names.length > 0) return `${base}, ${names.join(', ')}`;
  const parsed = extractFeaturedFromText(track.title, track.subtitle);
  if (parsed) return `${base}, ${parsed}`;
  return base;
}

function _enrichAlbumTrack(track: DiscoveryResult, album: DiscoveryResult): DiscoveryResult {
  return {
    ...track,
    image_url: track.image_url ?? album.image_url,
    extras: {
      ...track.extras,
      album: track.extras['album'] ?? album.title,
      album_artist: track.extras['album_artist'] ?? album.subtitle,
    },
  };
}

function _isTrackOwned(title: string, ownedTitles: Set<string>): boolean {
  return ownedTitles.has(title.toLowerCase().trim());
}

export function AlbumDetailBody({ result, detailRoute, isFromLibrary }: { result: DiscoveryResult; detailRoute: string; isFromLibrary?: boolean }): ReactElement {
  const router = useRouter();
  const queryClient = useQueryClient();
  const theme = useTheme();
  const save = useSaveTrack();
  const source = !isFromLibrary ? result.sources[0] : undefined;
  const deezerSource = !isFromLibrary
    ? result.sources.find((s) => s.provider === 'deezer')
    : undefined;
  const effectiveSource = deezerSource ?? source;
  const hasSources = effectiveSource !== undefined;

  const { tracks: apiTracks, isLoading: apiLoading, isError: apiError, refetch } = useAlbumTracks({
    provider: effectiveSource?.provider ?? 'deezer',
    externalId: effectiveSource?.external_id ?? '_',
    albumTitle: result.title,
    albumArtist: result.subtitle,
    allSources: result.sources,
    enabled: hasSources || (result.title !== ''),
  });

  const localTracks = useLibraryTracksForAlbum(result.title, result.subtitle);
  const localAsDiscovery = localTracks.map(libraryTrackToDiscoveryResult);

  const [moreExpanded, setMoreExpanded] = useState(false);
  const [saveAllTapped, setSaveAllTapped] = useState(false);

  const discovery = useAlbumDiscovery({
    albumTitle: result.title,
    artist: result.subtitle,
    enabled: !hasSources && moreExpanded,
  });

  const ownedTitles = new Set(localTracks.map((t) => t.title.toLowerCase().trim()));
  const moreTracks = discovery.tracks.filter((t) => !_isTrackOwned(t.title, ownedTitles));

  const tracks = hasSources ? apiTracks : localAsDiscovery;
  const isLoading = hasSources ? apiLoading : false;
  const isError = hasSources ? apiError : false;

  const onTrackPress = (track: DiscoveryResult): void => {
    setDetailHandoff(_enrichAlbumTrack(track, result));
    router.push(detailRoute as '/discover/detail');
  };

  const onQuickSave = (track: DiscoveryResult): void => {
    save.mutate(toCreateTrackRequest(_enrichAlbumTrack(track, result)));
  };

  const onSaveAll = (): void => {
    setSaveAllTapped(true);
    const allTracks = hasSources ? tracks : [...tracks, ...moreTracks];
    for (const track of allTracks) {
      const enriched = _enrichAlbumTrack(track, result);
      if (!_isTrackInLibraryCache(queryClient, enriched.title, enriched.subtitle)) {
        save.mutate(toCreateTrackRequest(enriched));
      }
    }
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
        <Button testID="detail-tracklist-retry" label="Retry" onPress={() => refetch()} style={sharedStyles.retryButton} />
      </View>
    );
  }

  if (tracks.length === 0 && !moreExpanded) {
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
    return sum + (trackExtras(t.extras).durationSeconds ?? 0);
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

  const allSaved = tracks.every((t) => _isTrackInLibraryCache(queryClient, t.title, t.subtitle));

  return (
    <View testID="detail-tracklist" style={styles.trackList}>
      {hasSources && !allSaved ? (
        <Button
          testID="detail-save-all"
          label={saveAllTapped ? 'Saving...' : 'Save All to Library'}
          onPress={onSaveAll}
          disabled={saveAllTapped}
          loading={saveAllTapped && save.isPending}
          style={styles.saveAllButton}
        />
      ) : null}

      {tracks.map((track, index) => (
        <AlbumTrackRow
          key={track.sources[0]?.external_id ?? `local-${index}`}
          track={track}
          index={index}
          subtitle={_trackSubtitleWithFeaturing(track)}
          isSaved={_isTrackInLibraryCache(queryClient, track.title, track.subtitle)}
          onPress={() => onTrackPress(track)}
          onQuickSave={() => onQuickSave(track)}
        />
      ))}

      <Text testID="detail-album-meta" variant="label" tone="tertiary" style={styles.albumMeta}>
        {metaParts.join(' · ')}
      </Text>

      {!hasSources ? (
        <View style={styles.moreSection}>
          <Pressable
            testID="detail-more-from-album"
            onPress={() => setMoreExpanded((prev) => !prev)}
            accessibilityRole="button"
            accessibilityLabel={moreExpanded ? 'Collapse more tracks' : 'Show more from this album'}
            style={({ pressed }) => [styles.moreHeader, pressed ? { opacity: 0.6 } : null]}
          >
            <Text variant="label" tone="accent">
              More from this album
            </Text>
            {moreExpanded ? (
              <ChevronDown size={18} color={theme.color.accent} />
            ) : (
              <ChevronRight size={18} color={theme.color.accent} />
            )}
          </Pressable>

          {moreExpanded ? (
            discovery.isLoading ? (
              <View style={styles.moreLoading}>
                <ActivityIndicator size="small" />
              </View>
            ) : discovery.isError ? (
              <View style={styles.moreError}>
                <Text variant="caption" tone="secondary">
                  Couldn't load additional tracks.
                </Text>
                <Button label="Retry" onPress={() => discovery.refetch()} style={sharedStyles.retryButton} />
              </View>
            ) : moreTracks.length === 0 ? (
              <Text variant="caption" tone="tertiary" style={styles.moreEmpty}>
                You have all tracks from this album.
              </Text>
            ) : (
              <>
                {moreTracks.map((track, index) => (
                  <AlbumTrackRow
                    key={track.sources[0]?.external_id ?? `more-${index}`}
                    track={track}
                    index={tracks.length + index}
                    subtitle={_trackSubtitleWithFeaturing(track)}
                    isSaved={false}
                    onPress={() => onTrackPress(track)}
                    onQuickSave={() => onQuickSave(track)}
                  />
                ))}
                <Button
                  testID="detail-save-all-more"
                  label="Save All to Library"
                  onPress={onSaveAll}
                  style={styles.saveAllButton}
                />
              </>
            )
          ) : null}
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  placeholder: { marginTop: spacing['2xl'], alignItems: 'center' },
  trackList: { marginTop: spacing.lg },
  albumMeta: { marginTop: spacing.lg, textAlign: 'center' as const },
  saveAllButton: { marginBottom: spacing.md },
  moreSection: { marginTop: spacing.xl },
  moreHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: spacing.md,
  },
  moreLoading: { paddingVertical: spacing.lg, alignItems: 'center' },
  moreError: { paddingVertical: spacing.md, alignItems: 'center' },
  moreEmpty: { paddingVertical: spacing.md },
});

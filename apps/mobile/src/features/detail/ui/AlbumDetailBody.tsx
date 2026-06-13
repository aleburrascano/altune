import type { ReactElement } from 'react';
import { ActivityIndicator, StyleSheet, View } from 'react-native';

import { useRouter } from 'expo-router';

import { useQueryClient } from '@tanstack/react-query';

import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { setDetailHandoff } from '@shared/lib/detail-handoff';

import { extractFeaturedFromText } from '../extras';
import { useAlbumTracks } from '../hooks/useAlbumTracks';
import { useSaveTrack } from '../hooks/useSaveTrack';
import { toCreateTrackRequest } from '../save-cache';

import { _albumYear, _isTrackInLibraryCache, sharedStyles } from './helpers';
import { AlbumTrackRow } from './AlbumTrackRow';

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

/** Album body: track list fetched from provider API. */
export function AlbumDetailBody({ result, detailRoute }: { result: DiscoveryResult; detailRoute: string }): ReactElement {
  const router = useRouter();
  const queryClient = useQueryClient();
  const save = useSaveTrack();
  const source = result.sources[0];
  const { tracks, isLoading, isError, refetch } = useAlbumTracks({
    provider: source?.provider ?? '',
    externalId: source?.external_id ?? '',
    allSources: result.sources,
    enabled: source !== undefined,
  });

  const onTrackPress = (track: DiscoveryResult): void => {
    setDetailHandoff(_enrichAlbumTrack(track, result));
    router.push(detailRoute as '/discover/detail');
  };

  const onQuickSave = (track: DiscoveryResult): void => {
    save.mutate(toCreateTrackRequest(_enrichAlbumTrack(track, result)));
  };

  const onSaveAll = (): void => {
    for (const track of tracks) {
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
      <Button
        testID="detail-save-all"
        label="Save All to Library"
        onPress={onSaveAll}
        style={styles.saveAllButton}
      />
      {tracks.map((track, index) => (
        <AlbumTrackRow
          key={track.sources[0]?.external_id ?? index}
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
    </View>
  );
}

const styles = StyleSheet.create({
  placeholder: { marginTop: spacing['2xl'], alignItems: 'center' },
  trackList: { marginTop: spacing.lg },
  albumMeta: { marginTop: spacing.lg, textAlign: 'center' as const },
  saveAllButton: { marginBottom: spacing.md },
});

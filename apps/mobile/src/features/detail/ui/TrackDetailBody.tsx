import type { ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native';

import { useQueryClient } from '@tanstack/react-query';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { canPlay } from '../../playback/helpers/canPlay';
import { usePlayback } from '../../playback/hooks/usePlayback';

import { extractFeaturedFromText, trackInfoRows } from '../extras';
import { useSaveTrack } from '../hooks/useSaveTrack';
import { toCreateTrackRequest } from '../save-cache';

import { _isTrackInLibraryCache } from './helpers';
import { PlayIconButton } from './PlayIconButton';

import { spacing } from '@shared/ui/theme/tokens';

export type LateralNavHandle = {
  navigateTo: (query: string, kind: 'artist' | 'album' | 'track') => Promise<void>;
  state: 'idle' | 'searching';
  error: string | null;
  clearError: () => void;
};

/** Track body: info rows + an optimistic Save-to-library action. */
export function TrackDetailBody({
  result,
  lateralNav,
}: {
  result: DiscoveryResult;
  lateralNav: LateralNavHandle;
}): ReactElement {
  const save = useSaveTrack();
  const playback = usePlayback();
  const queryClient = useQueryClient();
  const alreadySaved = _isTrackInLibraryCache(queryClient, result.title, result.subtitle);
  const trackId = typeof result.extras['track_id'] === 'string' ? result.extras['track_id'] : null;
  const isPlayable = canPlay(typeof result.extras['acquisition_status'] === 'string' ? result.extras['acquisition_status'] : null) && trackId !== null;
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
      {isPlayable ? (
        <View style={styles.playRow}>
          <PlayIconButton
            testID="detail-play"
            isPlaying={playback.status === 'playing' && playback.track?.trackId === trackId}
            disabled={playback.status === 'loading'}
            onPress={() => {
              if (playback.status === 'playing' && playback.track?.trackId === trackId) {
                playback.pause();
              } else {
                void playback.play({
                  trackId: trackId!,
                  title: result.title,
                  artist: result.subtitle ?? '',
                  artworkUrl: result.image_url,
                });
              }
            }}
          />
        </View>
      ) : null}
      <Button
        testID="detail-save"
        label={alreadySaved || save.isSuccess ? 'Saved' : 'Save to Library'}
        onPress={onSave}
        disabled={!canSave || save.isPending || save.isSuccess || alreadySaved}
        loading={save.isPending}
        haptic
        style={styles.saveButton}
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

const styles = StyleSheet.create({
  info: { marginTop: spacing['2xl'], gap: spacing.md },
  infoRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    gap: spacing.lg,
    minHeight: 48,
  },
  featuredArtists: { flexDirection: 'row', flexWrap: 'wrap' },
  playRow: {
    alignItems: 'center',
    marginTop: spacing['2xl'],
  },
  saveButton: { marginTop: spacing.lg },
  lateralLoading: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.sm,
    marginTop: spacing.md,
  },
});

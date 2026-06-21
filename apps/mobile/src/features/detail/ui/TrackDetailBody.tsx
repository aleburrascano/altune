import type { ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { canPlay } from '@shared/playback/canPlay';
import { usePlayback } from '@shared/playback/usePlayback';

import { extractFeaturedFromText, trackInfoRows } from '../extras';
import { trackExtras } from '../extras-accessors';
import { useIsTrackSaved } from '../hooks/useIsTrackSaved';
import { useLibraryTrackMatch } from '../hooks/useLibraryTrackMatch';
import { useSaveTrack } from '../hooks/useSaveTrack';
import { toCreateTrackRequest } from '../save-cache';

import { isCurrentlyPlaying } from './helpers';
import { PlayIconButton } from './PlayIconButton';
import { RelatedTracksSection } from './RelatedTracksSection';

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
  detailRoute,
}: {
  result: DiscoveryResult;
  lateralNav: LateralNavHandle;
  detailRoute: string;
}): ReactElement {
  const save = useSaveTrack();
  const playback = usePlayback();
  const alreadySaved = useIsTrackSaved(result.title, result.subtitle);
  const libraryMatch = useLibraryTrackMatch(result.title, result.subtitle);
  const te = trackExtras(result.extras);
  const effectiveTrackId = te.trackId ?? libraryMatch?.id ?? null;
  const effectiveAcqStatus = te.acquisitionStatus ?? libraryMatch?.acquisition_status ?? null;
  const isPlayable = canPlay(effectiveAcqStatus) && effectiveTrackId !== null;
  const previewUrl = te.previewUrl;
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

  const albumName = te.album;

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
      {(() => {
        const source = isPlayable && effectiveTrackId !== null
          ? { kind: 'library' as const, trackId: effectiveTrackId }
          : previewUrl !== null
            ? { kind: 'preview' as const, previewUrl }
            : null;
        if (!source) return null;
        const playing = isCurrentlyPlaying(playback, source);
        return (
          <View style={styles.playRow}>
            <PlayIconButton
              testID={source.kind === 'library' ? 'detail-play' : 'detail-preview'}
              isPlaying={playing}
              disabled={playback.status === 'loading'}
              onPress={() => {
                if (playing) {
                  playback.pause();
                } else {
                  void playback.play({
                    source,
                    title: result.title,
                    artist: result.subtitle ?? '',
                    artworkUrl: result.image_url,
                    durationSeconds: te.durationSeconds ?? undefined,
                  });
                }
              }}
            />
            {source.kind === 'preview' ? (
              <Text variant="caption" tone="secondary" style={{ marginTop: spacing.xs }}>
                Preview
              </Text>
            ) : null}
          </View>
        );
      })()}
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
      <RelatedTracksSection result={result} detailRoute={detailRoute} />
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

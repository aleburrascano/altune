import type { ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { FeaturedArtist } from '@shared/api-client/types';

import { getDetailHandoffSearchId } from '@shared/lib/detail-handoff';
import { canPlay } from '@shared/playback/canPlay';
import { usePlayback } from '@shared/playback/usePlayback';
import { spacing } from '@shared/ui/theme/tokens';

import { extractFeaturedFromText, formatDuration } from '../extras';
import { trackExtras } from '../extras-accessors';
import { useIsTrackSaved } from '../hooks/useIsTrackSaved';
import { useLibraryTrackMatch } from '../hooks/useLibraryTrackMatch';
import { useReportWrongAlbum } from '../hooks/useReportWrongAlbum';
import { useSaveTrack } from '../hooks/useSaveTrack';
import { toCreateTrackRequest } from '../save-cache';

import { isCurrentlyPlaying } from './helpers';
import { GenrePills } from './GenrePills';
import { RelatedTracksSection } from './RelatedTracksSection';

export type LateralNavHandle = {
  navigateTo: (query: string, kind: 'artist' | 'album' | 'track') => Promise<void>;
  state: 'idle' | 'searching';
  error: string | null;
  clearError: () => void;
};

/** Track body: play / save actions, identity genres, and light navigation. */
export function TrackDetailBody({
  result,
  lateralNav,
  detailRoute,
  genres,
}: {
  result: DiscoveryResult;
  lateralNav: LateralNavHandle;
  detailRoute: string;
  genres: string[];
}): ReactElement {
  const save = useSaveTrack();
  const wrongAlbum = useReportWrongAlbum(result);
  const playback = usePlayback();
  const alreadySaved = useIsTrackSaved(result.title, result.subtitle);
  const libraryMatch = useLibraryTrackMatch(result.title, result.subtitle);
  const te = trackExtras(result.extras);
  const effectiveTrackId = te.trackId ?? libraryMatch?.id ?? null;
  const effectiveAcqStatus = te.acquisitionStatus ?? libraryMatch?.acquisition_status ?? null;
  const isPlayable = canPlay(effectiveAcqStatus) && effectiveTrackId !== null;
  const previewUrl = te.previewUrl;
  const duration = te.durationSeconds != null ? formatDuration(te.durationSeconds) : null;

  // `?? ''` guards against an absent subtitle arriving as `undefined` (the wire
  // omits an empty subtitle, despite the `string | null` type) — a bare
  // `!== null` check passes for undefined and then `.length` crashes the screen.
  const canSave = (result.subtitle ?? '').length > 0;
  const albumName = te.album;
  const featured: FeaturedArtist[] =
    te.featuredArtists.length > 0
      ? te.featuredArtists
      : (extractFeaturedFromText(result.title, result.subtitle)?.split(', ') ?? []).map((name) => ({
          name,
          mbid: null,
          deezer_id: null,
        }));

  const source =
    isPlayable && effectiveTrackId !== null
      ? ({ kind: 'library', trackId: effectiveTrackId } as const)
      : previewUrl !== null
        ? ({ kind: 'preview', previewUrl } as const)
        : null;
  const playing = source !== null && isCurrentlyPlaying(playback, source);
  const playLabel = playing
    ? 'Pause'
    : source?.kind === 'preview'
      ? 'Preview'
      : `Play${duration ? `  ·  ${duration}` : ''}`;

  const onSave = (): void => {
    if (!canSave) {
      return;
    }
    save.mutate(toCreateTrackRequest(result));
  };

  const onTogglePlay = (): void => {
    if (source === null) {
      return;
    }
    if (playing) {
      playback.pause();
    } else {
      void playback.play({
        source,
        title: result.title,
        artist: result.subtitle ?? '',
        artworkUrl: result.image_url,
        durationSeconds: te.durationSeconds ?? undefined,
        // Provenance: stamp the originating search_id (from the detail handoff)
        // and the result_signature so the play/completed events join back to the
        // search that produced them — without this the satisfaction signal and the
        // behavioral corpus get a play with no search context (the empty-corpus bug).
        searchId: getDetailHandoffSearchId() ?? undefined,
        resultSignature: result.result_signature ?? undefined,
      });
    }
  };

  const onAlbumPress = (): void => {
    if (albumName !== null && result.subtitle !== null) {
      void lateralNav.navigateTo(`${albumName} ${result.subtitle}`, 'album');
    }
  };

  return (
    <View testID="detail-track-info" style={styles.body}>
      <View style={styles.actions}>
        {source !== null ? (
          <View style={styles.flex}>
            <Button
              testID={source.kind === 'library' ? 'detail-play' : 'detail-preview'}
              variant="primary"
              label={playLabel}
              onPress={onTogglePlay}
              disabled={playback.status === 'loading'}
              haptic
            />
          </View>
        ) : null}
        <View style={styles.flex}>
          <Button
            testID="detail-save"
            variant={source !== null ? 'secondary' : 'primary'}
            label={alreadySaved || save.isSuccess ? 'Saved' : 'Save'}
            onPress={onSave}
            disabled={!canSave || save.isPending || save.isSuccess || alreadySaved}
            loading={save.isPending}
            haptic
          />
        </View>
      </View>

      <GenrePills genres={genres} />

      {albumName !== null ? (
        <Pressable
          testID="detail-info-album"
          onPress={onAlbumPress}
          disabled={lateralNav.state === 'searching'}
          accessibilityRole="link"
          accessibilityLabel={`View album ${albumName}`}
          accessibilityHint="Opens album detail"
          style={({ pressed }) => [styles.navRow, pressed ? { opacity: 0.6 } : null]}
        >
          <Text variant="label" tone="tertiary">
            Album
          </Text>
          <Text variant="body" tone="accent" numberOfLines={1} style={styles.navValue}>
            {albumName}  ›
          </Text>
        </Pressable>
      ) : null}

      {albumName !== null ? (
        <Pressable
          testID="detail-wrong-album"
          onPress={wrongAlbum.report}
          disabled={wrongAlbum.reported}
          accessibilityRole="button"
          accessibilityLabel="Report wrong album"
          accessibilityHint="Tells us this album is wrong for this track"
          hitSlop={8}
          style={({ pressed }) => [styles.wrongAlbum, pressed ? { opacity: 0.6 } : null]}
        >
          <Text variant="caption" tone="tertiary">
            {wrongAlbum.reported ? 'Thanks — noted' : 'Wrong album?'}
          </Text>
        </Pressable>
      ) : null}

      {featured.length > 0 ? (
        <View testID="detail-info-featuring" style={styles.navRow}>
          <Text variant="label" tone="tertiary">
            Featuring
          </Text>
          <View style={styles.featured}>
            {featured.map((f, i) => (
              <Pressable
                key={f.mbid ?? f.name}
                onPress={() => void lateralNav.navigateTo(f.name, 'artist')}
                disabled={lateralNav.state === 'searching'}
                accessibilityRole="link"
                accessibilityLabel={`View artist ${f.name}`}
              >
                {({ pressed }) => (
                  <Text variant="body" tone="accent" style={pressed ? { opacity: 0.6 } : undefined}>
                    {i > 0 ? `, ${f.name}` : f.name}
                  </Text>
                )}
              </Pressable>
            ))}
          </View>
        </View>
      ) : null}

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
  body: { marginTop: spacing.xl, gap: spacing.md },
  actions: { flexDirection: 'row', gap: spacing.md },
  flex: { flex: 1 },
  navRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    gap: spacing.lg,
    minHeight: 48,
  },
  navValue: { flexShrink: 1, textAlign: 'right' },
  wrongAlbum: { alignSelf: 'flex-end', paddingVertical: spacing.xs },
  featured: { flexDirection: 'row', flexWrap: 'wrap', flexShrink: 1, justifyContent: 'flex-end' },
  lateralLoading: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.sm,
    marginTop: spacing.md,
  },
});

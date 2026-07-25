import type { ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';
import { useRouter, type Href } from 'expo-router';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { FeaturedArtist } from '@shared/api-client/types';

import { getDetailHandoffSearchId } from '@shared/lib/detail-handoff';
import { usePlayback } from '@shared/playback/usePlayback';
import { radius, spacing } from '@shared/ui/theme/tokens';

import { resolveFeatured } from '../extras';
import { trackExtras } from '../extras-accessors';
import { useOwnedTrack } from '../hooks/useOwnedTrack';
import { useReportWrongAlbum } from '../hooks/useReportWrongAlbum';
import { useSaveTrack } from '../hooks/useSaveTrack';
import { featuringRouteFor, type DetailRoute } from '../navigation';
import { resolvePlaySource } from '../play-source';
import { toCreateTrackRequest } from '../save-cache';
import {
  saveControlLabel,
  saveControlState,
  saveControlText,
  type SaveControlState,
} from '../save-control-state';

import { isCurrentlyPlaying } from './helpers';
import { PlayButton } from './PlayButton';
import { RelatedTracksSection } from './RelatedTracksSection';
import { SaveGlyph } from './SaveGlyph';

export type LateralNavHandle = {
  navigateTo: (query: string, kind: 'artist' | 'album' | 'track') => Promise<void>;
  state: 'idle' | 'searching';
  error: string | null;
  clearError: () => void;
};

type SaveState = SaveControlState | 'disabled';

export function TrackDetailBody({
  result,
  lateralNav,
  detailRoute,
  deezerFeatured,
}: {
  result: DiscoveryResult;
  lateralNav: LateralNavHandle;
  detailRoute: DetailRoute;
  deezerFeatured?: FeaturedArtist[];
}): ReactElement {
  const theme = useTheme();
  const router = useRouter();
  const save = useSaveTrack();
  const wrongAlbum = useReportWrongAlbum(result);
  const playback = usePlayback();
  const te = trackExtras(result.extras);
  const owned = useOwnedTrack(te);

  const canSave = (result.subtitle ?? '').length > 0;
  const albumName = te.album;
  const featured = resolveFeatured(result.extras, deezerFeatured, result.title, result.subtitle);

  const source = resolvePlaySource(te, owned);
  const playing = source !== null && isCurrentlyPlaying(playback, source);
  const isPreview = source?.kind === 'preview';
  const playTestID = isPreview ? 'detail-preview' : 'detail-play';
  const playA11y = playing ? 'Pause' : isPreview ? 'Play preview' : 'Play';

  const saveState: SaveState = !canSave
    ? 'disabled'
    : save.isError
      ? 'failed'
      : save.isPending
        ? 'saving'
        : saveControlState(owned);
  const saveInteractive = saveState === 'add' || saveState === 'failed';
  const saveDisplayState: SaveControlState = saveState === 'disabled' ? 'add' : saveState;

  const onSave = (): void => {
    if (!saveInteractive) {
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
      <View style={styles.hero}>
        <PlayButton
          testID={playTestID}
          playing={playing}
          disabled={source === null || playback.status === 'loading'}
          onPress={onTogglePlay}
          accessibilityLabel={playA11y}
        />
        {isPreview ? (
          <Text variant="caption" tone="tertiary" style={styles.previewTag}>
            30s preview
          </Text>
        ) : null}

        <Pressable
          testID="detail-save"
          onPress={onSave}
          disabled={!saveInteractive}
          accessibilityRole="button"
          accessibilityLabel={saveControlLabel(saveDisplayState, result.title)}
          accessibilityState={{ disabled: !saveInteractive, busy: saveState === 'saving' }}
          style={({ pressed }) => [
            styles.save,
            { borderColor: theme.color.border },
            pressed && saveInteractive ? { opacity: 0.6 } : null,
          ]}
        >
          <SaveGlyph state={saveDisplayState} addSize={18} addTone="primary" />
          <Text variant="label" tone={saveState === 'ready' ? 'success' : 'primary'}>
            {saveControlText(saveDisplayState)}
          </Text>
        </Pressable>
      </View>

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
            {albumName} ›
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
                onPress={() =>
                  router.push({
                    pathname: featuringRouteFor(detailRoute),
                    params: {
                      name: f.name,
                      ...(f.mbid ? { mbid: f.mbid } : {}),
                      ...(f.deezer_id != null ? { deezer_id: String(f.deezer_id) } : {}),
                    },
                  } as unknown as Href)
                }
                accessibilityRole="link"
                accessibilityLabel={`Tracks featuring ${f.name}`}
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

      {save.isError ? (
        <Banner testID="detail-save-error" tone="danger">
          Couldn't save this track. Tap Retry.
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
  body: { marginTop: spacing.xl, gap: spacing.lg },
  hero: { alignItems: 'center', gap: spacing.md },
  previewTag: { letterSpacing: 0.6, textTransform: 'uppercase' },
  save: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    minHeight: 40,
    paddingHorizontal: spacing.xl,
    borderWidth: 1,
    borderRadius: radius.full,
  },
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

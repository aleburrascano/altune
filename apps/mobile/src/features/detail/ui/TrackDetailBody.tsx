import type { ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native';
import { ChevronRight, Pause, Play } from 'lucide-react-native';
import { useRouter, type Href } from 'expo-router';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Banner } from '@shared/ui/primitives/Banner';
import { Text } from '@shared/ui/primitives/Text';
import { minInteractiveHeight, radius, spacing, useTheme } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { FeaturedArtist } from '@shared/api-client/types';

import { getDetailHandoffSearchId } from '@shared/lib/detail-handoff';
import { formatDuration } from '@shared/lib/format';
import { usePlayback } from '@shared/playback/usePlayback';

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

import { DetailActions } from './DetailActions';
import { DetailFacts, type DetailFact } from './DetailFacts';
import { DetailScaffold, type DetailChrome } from './DetailScaffold';
import { isCurrentlyPlaying } from './helpers';
import { RelatedTracksSection } from './RelatedTracksSection';
import { SaveGlyph } from './SaveGlyph';
import { Section } from './Section';

export type LateralNavHandle = {
  navigateTo: (query: string, kind: 'artist' | 'album' | 'track') => Promise<void>;
  state: 'idle' | 'searching';
  error: string | null;
  clearError: () => void;
};

type SaveState = SaveControlState | 'disabled';

function releasedYear(mbYear: number | undefined, extrasYear: number | null): string | null {
  if (mbYear != null && mbYear > 0) return String(mbYear);
  return extrasYear != null ? String(extrasYear) : null;
}

export function TrackDetailBody({
  chrome,
  result,
  lateralNav,
  detailRoute,
  deezerFeatured,
  mbYear,
}: {
  chrome: DetailChrome;
  result: DiscoveryResult;
  lateralNav: LateralNavHandle;
  detailRoute: DetailRoute;
  deezerFeatured?: FeaturedArtist[];
  mbYear?: number;
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

  const saveState: SaveState = !canSave
    ? 'disabled'
    : save.isError
      ? 'failed'
      : save.isPending
        ? 'saving'
        : saveControlState(owned);
  const saveInteractive = saveState === 'add' || saveState === 'failed';
  const saveDisplayState: SaveControlState = saveState === 'disabled' ? 'add' : saveState;

  const year = releasedYear(mbYear, te.year);
  const facts: (DetailFact | null)[] = [
    te.durationSeconds != null && te.durationSeconds > 0
      ? { label: 'Length', value: formatDuration(te.durationSeconds) }
      : null,
    year !== null ? { label: 'Released', value: year } : null,
    source === null
      ? null
      : isPreview
        ? { label: 'Source', value: 'Preview', tone: 'warning' }
        : { label: 'Source', value: 'Library' },
  ];

  const onTogglePlay = (): void => {
    if (source === null) {
      return;
    }
    if (playing) {
      playback.pause();
      return;
    }
    void playback.play({
      source,
      title: result.title,
      artist: result.subtitle ?? '',
      artworkUrl: result.image_url,
      durationSeconds: te.durationSeconds ?? undefined,
      searchId: getDetailHandoffSearchId() ?? undefined,
      resultSignature: result.result_signature ?? undefined,
    });
  };

  const onSave = (): void => {
    if (!saveInteractive) {
      return;
    }
    save.mutate(toCreateTrackRequest(result));
  };

  const playLabel = playing ? 'Pause' : isPreview ? 'Play preview' : 'Play';

  return (
    <DetailScaffold
      {...chrome}
      facts={<DetailFacts facts={facts} testID="detail-track-facts" />}
      menuItems={
        albumName !== null
          ? [
              {
                label: wrongAlbum.reported ? 'Thanks — noted' : 'Wrong album?',
                onPress: wrongAlbum.report,
              },
            ]
          : []
      }
      actions={
        <DetailActions
          primary={{
            label: playLabel,
            icon: playing ? Pause : Play,
            onPress: onTogglePlay,
            disabled: source === null || playback.status === 'loading',
            testID: isPreview ? 'detail-preview' : 'detail-play',
            accessibilityLabel: playLabel,
          }}
          secondary={
            <Pressable
              testID="detail-save"
              onPress={onSave}
              disabled={!saveInteractive}
              accessibilityRole="button"
              accessibilityLabel={saveControlLabel(saveDisplayState, result.title)}
              accessibilityState={{ disabled: !saveInteractive, busy: saveState === 'saving' }}
              style={({ pressed }) => [
                styles.savePill,
                { borderColor: theme.color.border, backgroundColor: theme.color.surface1 },
                pressed && saveInteractive ? styles.pressed : null,
              ]}
            >
              <SaveGlyph state={saveDisplayState} addSize={18} addTone="accent" />
              <Text variant="label" tone={saveState === 'ready' ? 'success' : 'primary'}>
                {saveControlText(saveDisplayState)}
              </Text>
            </Pressable>
          }
        />
      }
    >
      <View testID="detail-track-info">
        {albumName !== null || featured.length > 0 ? (
          <Section label="Details">
            {albumName !== null ? (
              <Pressable
                testID="detail-info-album"
                onPress={() => {
                  if (result.subtitle !== null) {
                    void lateralNav.navigateTo(`${albumName} ${result.subtitle}`, 'album');
                  }
                }}
                disabled={lateralNav.state === 'searching'}
                accessibilityRole="link"
                accessibilityLabel={`View album ${albumName}`}
                accessibilityHint="Opens album detail"
                style={({ pressed }) => [
                  styles.navRow,
                  { borderBottomColor: theme.color.border },
                  pressed ? styles.pressed : null,
                ]}
              >
                <Artwork uri={result.image_url} size={36} radius={radius.sm} />
                <View style={styles.navText}>
                  <Text variant="overline" tone="tertiary">
                    ALBUM
                  </Text>
                  <Text variant="body" numberOfLines={1}>
                    {albumName}
                  </Text>
                </View>
                <ChevronRight size={16} color={theme.color.textTertiary} />
              </Pressable>
            ) : null}

            {featured.length > 0 ? (
              <View testID="detail-info-featuring">
                {featured.map((f) => (
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
                    style={({ pressed }) => [
                      styles.navRow,
                      { borderBottomColor: theme.color.border },
                      pressed ? styles.pressed : null,
                    ]}
                  >
                    <Artwork uri={null} size={36} radius={radius.full} />
                    <View style={styles.navText}>
                      <Text variant="overline" tone="tertiary">
                        FEATURING
                      </Text>
                      <Text variant="body" numberOfLines={1}>
                        {f.name}
                      </Text>
                    </View>
                    <ChevronRight size={16} color={theme.color.textTertiary} />
                  </Pressable>
                ))}
              </View>
            ) : null}
          </Section>
        ) : null}

        {save.isError ? (
          <Banner testID="detail-save-error" tone="danger" style={styles.banner}>
            Couldn&apos;t save this track. Tap Retry.
          </Banner>
        ) : null}
        {lateralNav.error !== null ? (
          <Banner testID="detail-lateral-error" tone="danger" style={styles.banner}>
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
    </DetailScaffold>
  );
}

const styles = StyleSheet.create({
  savePill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    minHeight: minInteractiveHeight,
    paddingHorizontal: spacing.lg,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.full,
    flexShrink: 0,
  },
  navRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    minHeight: 56,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  navText: { flex: 1 },
  banner: { marginTop: spacing.lg },
  pressed: { opacity: 0.6 },
  lateralLoading: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.sm,
    marginTop: spacing.md,
  },
});

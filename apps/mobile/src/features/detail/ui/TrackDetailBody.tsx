import { type ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native';
import { ChevronRight, ListPlus, Pause, Play } from 'lucide-react-native';

import { AddToPlaylistSheet } from '@shared/playlists';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Banner } from '@shared/ui/primitives/Banner';
import { Text } from '@shared/ui/primitives/Text';
import { minInteractiveHeight, radius, spacing, useTheme } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { FeaturedArtist } from '@shared/api-client/types';

import { formatDuration } from '@shared/lib/format';

import { useTrackDetailActions, type LateralNavHandle } from '../hooks/useTrackDetailActions';
import { type DetailRoute } from '../navigation';
import { saveControlLabel, saveControlText, saveFailureBanner } from '../save-control-state';

import { sharedStyles } from './styles';
import { DetailActions, SecondaryAction } from './DetailActions';
import { DetailFacts, type DetailFact } from './DetailFacts';
import { DetailScaffold, type DetailChrome } from './DetailScaffold';
import { RelatedTracksSection } from './RelatedTracksSection';
import { SaveGlyph } from './SaveGlyph';
import { Section } from './Section';

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
  const actions = useTrackDetailActions({ result, lateralNav, detailRoute, deezerFeatured, mbYear });

  const facts: (DetailFact | null)[] = [
    actions.durationSeconds != null && actions.durationSeconds > 0
      ? { label: 'Length', value: formatDuration(actions.durationSeconds) }
      : null,
    actions.year !== null ? { label: 'Released', value: actions.year } : null,
    actions.source === null
      ? null
      : actions.isPreview
        ? { label: 'Source', value: 'Preview', tone: 'warning' }
        : { label: 'Source', value: 'Library' },
  ];

  return (
    <DetailScaffold
      {...chrome}
      facts={<DetailFacts facts={facts} testID="detail-track-facts" />}
      menuItems={
        actions.albumName !== null
          ? [
              {
                label: actions.wrongAlbumReported ? 'Thanks — noted' : 'Wrong album?',
                onPress: actions.onReportWrongAlbum,
              },
            ]
          : []
      }
      actions={
        <DetailActions
          primary={{
            label: actions.playLabel,
            icon: actions.playing ? Pause : Play,
            onPress: actions.onTogglePlay,
            disabled: actions.source === null || actions.playLoading,
            testID: actions.isPreview ? 'detail-preview' : 'detail-play',
            accessibilityLabel: actions.playLabel,
          }}
          secondary={
            <>
              <Pressable
                testID="detail-save"
                onPress={actions.onSave}
                disabled={!actions.saveInteractive}
                accessibilityRole="button"
                accessibilityLabel={saveControlLabel(actions.saveDisplayState, result.title)}
                accessibilityState={{
                  disabled: !actions.saveInteractive,
                  busy: actions.saveState === 'saving',
                }}
                style={({ pressed }) => [
                  styles.savePill,
                  { borderColor: theme.color.border, backgroundColor: theme.color.surface1 },
                  pressed && actions.saveInteractive ? sharedStyles.pressed : null,
                ]}
              >
                <SaveGlyph state={actions.saveDisplayState} addSize={18} />
                <Text variant="label" tone={actions.saveState === 'ready' ? 'success' : 'primary'}>
                  {saveControlText(actions.saveDisplayState)}
                </Text>
              </Pressable>
              {actions.canSave ? (
                <SecondaryAction
                  testID="detail-add-to-playlist"
                  icon={ListPlus}
                  onPress={() => actions.setPlaylistSheetVisible(true)}
                  accessibilityLabel={`Add ${result.title} to a playlist`}
                />
              ) : null}
            </>
          }
        />
      }
    >
      <View testID="detail-track-info">
        {actions.albumName !== null || actions.featured.length > 0 ? (
          <Section label="Details">
            {actions.albumName !== null ? (
              <Pressable
                testID="detail-info-album"
                onPress={actions.onAlbumPress}
                disabled={lateralNav.state === 'searching'}
                accessibilityRole="link"
                accessibilityLabel={`View album ${actions.albumName}`}
                accessibilityHint="Opens album detail"
                style={({ pressed }) => [
                  styles.navRow,
                  { borderBottomColor: theme.color.border },
                  pressed ? sharedStyles.pressed : null,
                ]}
              >
                <Artwork uri={result.image_url} size={36} radius={radius.sm} />
                <View style={styles.navText}>
                  <Text variant="overline" tone="tertiary">
                    ALBUM
                  </Text>
                  <Text variant="body" numberOfLines={1}>
                    {actions.albumName}
                  </Text>
                </View>
                <ChevronRight size={16} color={theme.color.textTertiary} />
              </Pressable>
            ) : null}

            {actions.featured.length > 0 ? (
              <View testID="detail-info-featuring">
                {actions.featured.map((f) => (
                  <Pressable
                    key={f.mbid ?? f.name}
                    onPress={() => actions.onFeaturedPress(f)}
                    accessibilityRole="link"
                    accessibilityLabel={`Tracks featuring ${f.name}`}
                    style={({ pressed }) => [
                      styles.navRow,
                      { borderBottomColor: theme.color.border },
                      pressed ? sharedStyles.pressed : null,
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

        {actions.saveFailure !== null ? (
          <Banner testID="detail-save-error" tone="danger" style={styles.banner}>
            {saveFailureBanner(actions.saveFailure)}
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

        <AddToPlaylistSheet
          visible={actions.playlistSheetVisible}
          label={`${result.title}${result.subtitle != null ? ` — ${result.subtitle}` : ''}`}
          resolveTrackIds={actions.resolveTrackIds}
          onClose={() => actions.setPlaylistSheetVisible(false)}
        />
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
  lateralLoading: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.sm,
    marginTop: spacing.md,
  },
});

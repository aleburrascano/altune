import { type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { ChevronDown, ChevronRight, Play, Plus } from 'lucide-react-native';

import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { minInteractiveHeight, radius, spacing, useTheme } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { extractFeaturedFromText } from '../extras';
import { trackExtras } from '../extras-accessors';
import { useAlbumDetailState } from '../hooks/useAlbumDetailState';
import type { DetailRoute } from '../navigation';

import { _albumYear, formatRuntime, sharedStyles } from './helpers';
import { AlbumTrackRow } from './AlbumTrackRow';
import { DetailActions } from './DetailActions';
import { DetailFacts, type DetailFact } from './DetailFacts';
import { DetailScaffold, type DetailChrome } from './DetailScaffold';
import { TrackRowsSkeleton } from './DetailSkeleton';
import { Section } from './Section';

function _trackSubtitleWithFeaturing(track: DiscoveryResult): string {
  const base = track.subtitle ?? '';
  const names = trackExtras(track.extras).featuredArtists.map((f) => f.name);
  if (names.length > 0) return `${base}, ${names.join(', ')}`;
  const parsed = extractFeaturedFromText(track.title, track.subtitle);
  if (parsed) return `${base}, ${parsed}`;
  return base;
}

export function AlbumDetailBody({
  chrome,
  result,
  detailRoute,
  isFromLibrary,
  mbYear,
}: {
  chrome: DetailChrome;
  result: DiscoveryResult;
  detailRoute: DetailRoute;
  isFromLibrary?: boolean;
  mbYear?: number;
}): ReactElement {
  const theme = useTheme();
  const album = useAlbumDetailState(result, detailRoute, isFromLibrary);

  const runtimeSeconds = album.tracks.reduce(
    (sum, t) => sum + (trackExtras(t.extras).durationSeconds ?? 0),
    0,
  );
  const runtime = formatRuntime(runtimeSeconds);
  const year = mbYear != null && mbYear > 0 ? String(mbYear) : _albumYear(result);

  const facts: (DetailFact | null)[] = [
    album.tracks.length > 0 ? { label: 'Tracks', value: String(album.tracks.length) } : null,
    runtime !== null ? { label: 'Runtime', value: runtime } : null,
    year !== null ? { label: 'Released', value: year } : null,
  ];

  return (
    <DetailScaffold
      {...chrome}
      facts={<DetailFacts facts={facts} testID="detail-album-meta" />}
      actions={
        <DetailActions
          primary={{
            label: album.playButton.label,
            icon: Play,
            onPress: album.onPlayOwned,
            disabled: album.playButton.disabled,
            testID: 'detail-album-play',
            accessibilityLabel: album.playButton.label,
          }}
          secondary={
            album.owned.unownedCount > 0 ? (
              <Pressable
                testID="detail-save-all"
                onPress={album.onSaveAll}
                disabled={album.saveAllTapped}
                accessibilityRole="button"
                accessibilityLabel={`Save ${album.owned.unownedCount} tracks to your library`}
                accessibilityState={{ disabled: album.saveAllTapped }}
                style={({ pressed }) => [
                  styles.savePill,
                  { borderColor: theme.color.border, backgroundColor: theme.color.surface1 },
                  pressed && !album.saveAllTapped ? styles.pressed : null,
                ]}
              >
                <Plus size={18} color={theme.color.accent} />
                <Text variant="label">
                  {album.saveAllTapped ? 'Saving…' : `Save ${album.owned.unownedCount}`}
                </Text>
              </Pressable>
            ) : null
          }
        />
      }
    >
      {renderTracks()}
    </DetailScaffold>
  );

  function renderTracks(): ReactElement {
    if (album.isLoading) {
      return (
        <Section label="Tracks">
          <TrackRowsSkeleton testID="detail-tracklist-loading" />
        </Section>
      );
    }

    if (album.isError) {
      return (
        <Section label="Tracks">
          <View testID="detail-tracklist-error" style={styles.placeholder}>
            <Text variant="body" tone="danger">
              Couldn&apos;t load tracks.
            </Text>
            <Button
              testID="detail-tracklist-retry"
              label="Retry"
              onPress={() => album.refetch()}
              style={sharedStyles.retryButton}
            />
          </View>
        </Section>
      );
    }

    if (album.tracks.length === 0 && !album.moreExpanded) {
      return (
        <Section label="Tracks">
          <View testID="detail-tracklist-empty" style={styles.placeholder}>
            <Text variant="body" tone="tertiary">
              No tracks found.
            </Text>
          </View>
        </Section>
      );
    }

    return (
      <View testID="detail-tracklist">
        <Section label="Tracks">
          {album.tracks.map((track, index) => (
            <AlbumTrackRow
              key={track.sources[0]?.external_id ?? `local-${index}`}
              track={track}
              index={index}
              subtitle={_trackSubtitleWithFeaturing(track)}
              saveState={album.saveStateFor(track)}
              onPress={() => album.onTrackPress(track)}
              onQuickSave={() => album.onQuickSave(track)}
            />
          ))}
        </Section>

        {!album.hasSources && album.moreTracks.length > 0 ? (
          <View style={styles.moreSection}>
            <Pressable
              testID="detail-more-from-album"
              onPress={() => album.setMoreExpanded((prev) => !prev)}
              accessibilityRole="button"
              accessibilityLabel={
                album.moreExpanded ? 'Collapse more tracks' : 'Show more from this album'
              }
              style={({ pressed }) => [styles.moreHeader, pressed ? styles.pressed : null]}
            >
              <Text variant="label" tone="accent">
                More from this album
              </Text>
              {album.moreExpanded ? (
                <ChevronDown size={18} color={theme.color.accent} />
              ) : (
                <ChevronRight size={18} color={theme.color.accent} />
              )}
            </Pressable>

            {album.moreExpanded ? (
              <>
                {album.moreTracks.map((track, index) => (
                  <AlbumTrackRow
                    key={track.sources[0]?.external_id ?? `more-${index}`}
                    track={track}
                    index={album.tracks.length + index}
                    subtitle={_trackSubtitleWithFeaturing(track)}
                    saveState={album.saveStateFor(track)}
                    onPress={() => album.onTrackPress(track)}
                    onQuickSave={() => album.onQuickSave(track)}
                  />
                ))}
                <Button
                  testID="detail-save-all-more"
                  label="Save all"
                  variant="secondary"
                  onPress={album.onSaveAll}
                  style={styles.moreSaveAll}
                />
              </>
            ) : null}
          </View>
        ) : null}
      </View>
    );
  }
}

const styles = StyleSheet.create({
  placeholder: { alignItems: 'center', paddingVertical: spacing.lg },
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
  moreSaveAll: { marginTop: spacing.lg },
  moreSection: { marginTop: spacing.xl },
  moreHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: spacing.md,
    minHeight: minInteractiveHeight,
  },
  pressed: { opacity: 0.6 },
});

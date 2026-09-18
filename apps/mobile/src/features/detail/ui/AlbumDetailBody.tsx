import { type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Play, Plus } from 'lucide-react-native';

import { Text } from '@shared/ui/primitives/Text';
import { minInteractiveHeight, radius, spacing, useTheme } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { asyncView } from '@shared/lib/async-view';
import { AsyncSection } from '@shared/ui/AsyncSection';

import { trackExtras } from '../extras-accessors';
import { useAlbumDetailState } from '../hooks/useAlbumDetailState';
import type { DetailRoute } from '../navigation';

import { albumYear, formatRuntime, trackSubtitleWithFeaturing } from './formatters';
import { sharedStyles } from './styles';
import { AlbumMoreTracks } from './AlbumMoreTracks';
import { AlbumTrackRow } from './AlbumTrackRow';
import { DetailActions } from './DetailActions';
import { DetailFacts, type DetailFact } from './DetailFacts';
import { DetailScaffold, type DetailChrome } from './DetailScaffold';
import { TrackRowsSkeleton } from './DetailSkeleton';
import { Section } from './Section';
import { SectionError } from './SectionError';

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
  const year = mbYear != null && mbYear > 0 ? String(mbYear) : albumYear(result);

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
                disabled={album.savingAll}
                accessibilityRole="button"
                accessibilityLabel={`Save ${album.owned.unownedCount} tracks to your library`}
                accessibilityState={{ disabled: album.savingAll }}
                style={({ pressed }) => [
                  styles.savePill,
                  { borderColor: theme.color.border, backgroundColor: theme.color.surface1 },
                  pressed && !album.savingAll ? sharedStyles.pressed : null,
                ]}
              >
                <Plus size={18} color={theme.color.accent} />
                <Text variant="label">
                  {album.savingAll ? 'Saving…' : `Save ${album.owned.unownedCount}`}
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
    return (
      <AsyncSection
        view={asyncView({
          isLoading: album.isLoading,
          isError: album.isError,
          isEmpty: album.tracks.length === 0 && !album.moreExpanded && !album.discoveryError,
        })}
        skeleton={() => (
          <Section label="Tracks">
            <TrackRowsSkeleton testID="detail-tracklist-loading" />
          </Section>
        )}
        error={() => (
          <Section label="Tracks">
            <SectionError
              testIDPrefix="detail-tracklist"
              message="Couldn't load tracks."
              onRetry={() => album.refetch()}
            />
          </Section>
        )}
        empty={() => (
          <Section label="Tracks">
            <View testID="detail-tracklist-empty" style={styles.placeholder}>
              <Text variant="body" tone="tertiary">
                No tracks found.
              </Text>
            </View>
          </Section>
        )}
      >
        <View testID="detail-tracklist">
          <Section label="Tracks">
            {album.tracks.map((track, index) => (
              <AlbumTrackRow
                key={track.sources[0]?.external_id ?? `local-${index}`}
                track={track}
                index={index}
                subtitle={trackSubtitleWithFeaturing(track)}
                owned={album.ownedFor(track)}
                onPress={() => album.onTrackPress(track)}
                onQuickSave={() => album.onQuickSave(track)}
              />
            ))}
          </Section>

          {!album.hasSources ? (
            <AlbumMoreTracks
              tracks={album.moreTracks}
              baseIndex={album.tracks.length}
              expanded={album.moreExpanded}
              onToggle={() => album.setMoreExpanded((prev) => !prev)}
              savingAll={album.savingAll}
              onSaveAll={album.onSaveAll}
              ownedFor={album.ownedFor}
              onTrackPress={album.onTrackPress}
              onQuickSave={album.onQuickSave}
              isError={album.discoveryError}
              onRetry={album.discoveryRefetch}
            />
          ) : null}
        </View>
      </AsyncSection>
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
});

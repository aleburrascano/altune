import { type ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native';

import { ChevronDown, ChevronRight } from 'lucide-react-native';

import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';
import { spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { extractFeaturedFromText } from '../extras';
import { trackExtras } from '../extras-accessors';
import { useAlbumDetailState } from '../hooks/useAlbumDetailState';

import { _albumYear, sharedStyles } from './helpers';
import { AlbumTrackRow } from './AlbumTrackRow';

function _trackSubtitleWithFeaturing(track: DiscoveryResult): string {
  const base = track.subtitle ?? '';
  const names = trackExtras(track.extras).featuredArtists.map((f) => f.name);
  if (names.length > 0) return `${base}, ${names.join(', ')}`;
  const parsed = extractFeaturedFromText(track.title, track.subtitle);
  if (parsed) return `${base}, ${parsed}`;
  return base;
}

export function AlbumDetailBody({ result, detailRoute, isFromLibrary }: { result: DiscoveryResult; detailRoute: string; isFromLibrary?: boolean }): ReactElement {
  const theme = useTheme();
  const album = useAlbumDetailState(result, detailRoute, isFromLibrary);

  if (album.isLoading) {
    return (
      <View testID="detail-tracklist-loading" style={styles.placeholder}>
        <ActivityIndicator />
      </View>
    );
  }

  if (album.isError) {
    return (
      <View testID="detail-tracklist-error" style={styles.placeholder}>
        <Text variant="body" tone="danger">
          Couldn't load tracks.
        </Text>
        <Button testID="detail-tracklist-retry" label="Retry" onPress={() => album.refetch()} style={sharedStyles.retryButton} />
      </View>
    );
  }

  if (album.tracks.length === 0 && !album.moreExpanded) {
    return (
      <View testID="detail-tracklist-empty" style={styles.placeholder}>
        <Text variant="body" tone="tertiary">
          No tracks found.
        </Text>
      </View>
    );
  }

  const albumYear = _albumYear(result);
  const totalDurationSec = album.tracks.reduce((sum, t) => {
    return sum + (trackExtras(t.extras).durationSeconds ?? 0);
  }, 0);
  const metaParts: string[] = [];
  if (albumYear) metaParts.push(albumYear);
  metaParts.push(`${album.tracks.length} track${album.tracks.length !== 1 ? 's' : ''}`);
  if (totalDurationSec > 0) {
    const totalMin = Math.floor(totalDurationSec / 60);
    const hrs = Math.floor(totalMin / 60);
    const mins = totalMin % 60;
    metaParts.push(hrs > 0 ? `${hrs} hr ${mins} min` : `${mins} min`);
  }

  const allSaved = album.tracks.every((t) => album.isSaved(t.title, t.subtitle));

  return (
    <View testID="detail-tracklist" style={styles.trackList}>
      {album.hasSources && !allSaved ? (
        <Button
          testID="detail-save-all"
          label={album.saveAllTapped ? 'Saving...' : 'Save All to Library'}
          onPress={album.onSaveAll}
          disabled={album.saveAllTapped}
          loading={album.saveAllTapped && album.savePending}
          style={styles.saveAllButton}
        />
      ) : null}

      <Text variant="label" tone="tertiary" style={styles.tracksTitle}>
        Tracks
      </Text>

      {album.tracks.map((track, index) => (
        <AlbumTrackRow
          key={track.sources[0]?.external_id ?? `local-${index}`}
          track={track}
          index={index}
          subtitle={_trackSubtitleWithFeaturing(track)}
          saveState={album.saveStateFor(track.title, track.subtitle)}
          onPress={() => album.onTrackPress(track)}
          onQuickSave={() => album.onQuickSave(track)}
        />
      ))}

      <Text testID="detail-album-meta" variant="label" tone="tertiary" style={styles.albumMeta}>
        {metaParts.join(' · ')}
      </Text>

      {!album.hasSources && album.moreTracks.length > 0 ? (
        <View style={styles.moreSection}>
          <Pressable
            testID="detail-more-from-album"
            onPress={() => album.setMoreExpanded((prev) => !prev)}
            accessibilityRole="button"
            accessibilityLabel={album.moreExpanded ? 'Collapse more tracks' : 'Show more from this album'}
            style={({ pressed }) => [styles.moreHeader, pressed ? { opacity: 0.6 } : null]}
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
                  saveState={album.saveStateFor(track.title, track.subtitle)}
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

const styles = StyleSheet.create({
  placeholder: { marginTop: spacing['2xl'], alignItems: 'center' },
  trackList: { marginTop: spacing.lg },
  tracksTitle: { marginTop: spacing.xl, marginBottom: spacing.sm },
  albumMeta: { marginTop: spacing.lg, textAlign: 'center' as const },
  saveAllButton: { marginBottom: spacing.md },
  moreSaveAll: { marginTop: spacing.lg },
  moreSection: { marginTop: spacing.xl },
  moreHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: spacing.md,
  },
});

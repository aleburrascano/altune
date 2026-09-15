import { type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { ChevronDown, ChevronRight } from 'lucide-react-native';

import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { minInteractiveHeight, spacing, useTheme } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { type OwnedTrack } from '../hooks/useOwnedTrack';

import { trackSubtitleWithFeaturing } from './formatters';
import { AlbumTrackRow } from './AlbumTrackRow';
import { SectionError } from './SectionError';

export function AlbumMoreTracks({
  tracks,
  baseIndex,
  expanded,
  onToggle,
  savingAll,
  onSaveAll,
  ownedFor,
  onTrackPress,
  onQuickSave,
  isError,
  onRetry,
}: {
  tracks: DiscoveryResult[];
  baseIndex: number;
  expanded: boolean;
  onToggle: () => void;
  savingAll: boolean;
  onSaveAll: () => void;
  ownedFor: (track: DiscoveryResult) => OwnedTrack | null;
  onTrackPress: (track: DiscoveryResult) => void;
  onQuickSave: (track: DiscoveryResult) => void;
  isError: boolean;
  onRetry: () => void;
}): ReactElement | null {
  const theme = useTheme();

  // "Found the album but couldn't list its tracks" — surface it with a retry
  // instead of silently omitting the section, which hid the failure entirely.
  if (isError) {
    return (
      <View style={styles.moreSection}>
        <View style={styles.moreHeader}>
          <Text variant="label" tone="accent">
            More from this album
          </Text>
        </View>
        <SectionError
          testIDPrefix="detail-more-from-album"
          message="Couldn't load more tracks."
          onRetry={onRetry}
        />
      </View>
    );
  }

  if (tracks.length === 0) return null;

  return (
    <View style={styles.moreSection}>
      <Pressable
        testID="detail-more-from-album"
        onPress={onToggle}
        accessibilityRole="button"
        accessibilityLabel={expanded ? 'Collapse more tracks' : 'Show more from this album'}
        style={({ pressed }) => [styles.moreHeader, pressed ? styles.pressed : null]}
      >
        <Text variant="label" tone="accent">
          More from this album
        </Text>
        {expanded ? (
          <ChevronDown size={18} color={theme.color.accent} />
        ) : (
          <ChevronRight size={18} color={theme.color.accent} />
        )}
      </Pressable>

      {expanded ? (
        <>
          {tracks.map((track, index) => (
            <AlbumTrackRow
              key={track.sources[0]?.external_id ?? `more-${index}`}
              track={track}
              index={baseIndex + index}
              subtitle={trackSubtitleWithFeaturing(track)}
              owned={ownedFor(track)}
              onPress={() => onTrackPress(track)}
              onQuickSave={() => onQuickSave(track)}
            />
          ))}
          <Button
            testID="detail-save-all-more"
            label={savingAll ? 'Saving…' : 'Save all'}
            variant="secondary"
            onPress={onSaveAll}
            disabled={savingAll}
            style={styles.moreSaveAll}
          />
        </>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
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

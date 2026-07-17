/**
 * DiscoverRow — the art-forward result row, Spotify-style (discover-music-v2).
 *
 * testID = `discover-row-<kind>-<position>` (preserved). Confidence is gone:
 * no ConfidenceDot, no verified glow. Row shape varies by kind:
 *   - artist: circular artwork, "Artist" label (subtitle is null)
 *   - album:  square artwork, artist subtitle + year (from extras.year)
 *   - track:  square artwork, artist subtitle
 */

import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Pause, Play } from 'lucide-react-native';
import * as Haptics from 'expo-haptics';

import { Row, Text, radius, spacing, useTheme } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { featuredArtistsFromExtras, withFeaturing } from '@shared/lib/featured';

import { kindLabel } from '../state';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import { usePlayback } from '@shared/playback/usePlayback';
import { getPreviewUrl } from '@shared/playback/previewUrl';

export type DiscoverRowProps = {
  result: DiscoveryResult;
  position: number;
  onPress: (result: DiscoveryResult, position: number) => void;
};

const ART_SIZE = 56;

function _secondaryLine(result: DiscoveryResult): string {
  const kind = kindLabel(result.kind);

  if (result.kind === 'artist') {
    return kind;
  }
  if (result.kind === 'album') {
    const year = result.extras['year'];
    const parts = [kind];
    if (result.subtitle) parts.push(result.subtitle);
    if (typeof year === 'number' || typeof year === 'string') parts.push(String(year));
    return parts.join(' · ');
  }
  const parts = [kind];
  if (result.subtitle) {
    const guests = featuredArtistsFromExtras(result.extras['featured_artists']);
    parts.push(withFeaturing(result.subtitle, guests));
  }
  const count = result.extras['variant_count'];
  if (typeof count === 'number' && count > 1) {
    parts.push(`+${count - 1} version${count > 2 ? 's' : ''}`);
  }
  return parts.join(' · ');
}

export function DiscoverRow({ result, position, onPress }: DiscoverRowProps): ReactElement {
  const testId = `discover-row-${result.kind}-${position}`;
  const isArtist = result.kind === 'artist';
  const secondary = _secondaryLine(result);
  const a11yLabel = `${result.title}${secondary ? `, ${secondary}` : ''}`;
  const theme = useTheme();
  const playback = usePlayback();
  const previewUrl = result.kind === 'track' ? getPreviewUrl(result.extras) : null;

  const isThisPreviewPlaying =
    previewUrl !== null &&
    playback.track?.source.kind === 'preview' &&
    playback.track.source.previewUrl === previewUrl &&
    playback.status === 'playing';

  const onPreviewPress = (): void => {
    if (previewUrl === null) return;
    void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    if (isThisPreviewPlaying) {
      playback.pause();
    } else {
      void playback.play({
        source: { kind: 'preview', previewUrl },
        title: result.title,
        artist: result.subtitle ?? '',
        artworkUrl: result.image_url,
      });
    }
  };

  return (
    <Pressable
      testID={testId}
      onPress={() => onPress(result, position)}
      accessibilityRole="button"
      accessibilityLabel={a11yLabel}
      style={({ pressed }) => [styles.row, pressed ? { opacity: 0.7, backgroundColor: theme.color.surface1 } : null]}
    >
      <Row
        leading={
          <Artwork
            uri={result.image_url}
            size={ART_SIZE}
            radius={isArtist ? radius.full : radius.md}
            accessibilityLabel={result.title}
          />
        }
        trailing={
          previewUrl !== null ? (
            <View style={[styles.previewWrap, { backgroundColor: theme.color.surface2 }]}>
              <IconButton
                testID={`discover-preview-${position}`}
                icon={isThisPreviewPlaying ? Pause : Play}
                size={18}
                onPress={onPreviewPress}
                accessibilityLabel={isThisPreviewPlaying ? 'Pause preview' : 'Play preview'}
              />
            </View>
          ) : undefined
        }
      >
        <Text variant="bodyStrong" numberOfLines={1}>
          {result.title}
        </Text>
        <Text variant="label" tone="secondary" numberOfLines={1} style={styles.secondary}>
          {secondary}
        </Text>
      </Row>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  row: {
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.xs,
    borderRadius: radius.md,
  },
  secondary: { marginTop: spacing.xs },
  previewWrap: {
    width: 36,
    height: 36,
    borderRadius: 18,
    alignItems: 'center' as const,
    justifyContent: 'center' as const,
    overflow: 'hidden' as const,
  },
});

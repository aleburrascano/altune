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
import { Pressable } from 'react-native';
import { Pause, Play } from 'lucide-react-native';

import { Artwork, Card, Row, Text, radius, spacing } from '@shared/ui';
import { IconButton } from '@shared/ui/primitives/IconButton';

import type { DiscoveryResult } from '../../../shared/api-client/discovery';
import { usePlayback } from '@shared/playback/usePlayback';
import { getPreviewUrl } from '@shared/playback/previewUrl';

export type DiscoverRowProps = {
  result: DiscoveryResult;
  position: number;
  onPress: (result: DiscoveryResult, position: number) => void;
};

const ART_SIZE = 52;

/** Secondary line under the title, by kind. Null → render nothing. */
function _secondaryLine(result: DiscoveryResult): string | null {
  if (result.kind === 'artist') {
    return 'Artist';
  }
  if (result.kind === 'album') {
    const year = result.extras['year'];
    if (typeof year === 'number' || typeof year === 'string') {
      return result.subtitle !== null ? `${result.subtitle} · ${year}` : `${year}`;
    }
  }
  return result.subtitle;
}

export function DiscoverRow({ result, position, onPress }: DiscoverRowProps): ReactElement {
  const testId = `discover-row-${result.kind}-${position}`;
  const isArtist = result.kind === 'artist';
  const secondary = _secondaryLine(result);
  const a11yLabel = `${result.title}${secondary ? `, ${secondary}` : ''}`;
  const playback = usePlayback();
  const previewUrl = result.kind === 'track' ? getPreviewUrl(result.extras) : null;

  const isThisPreviewPlaying =
    previewUrl !== null &&
    playback.track?.source.kind === 'preview' &&
    playback.track.source.previewUrl === previewUrl &&
    playback.status === 'playing';

  const onPreviewPress = (): void => {
    if (previewUrl === null) return;
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
      style={({ pressed }) => (pressed ? { opacity: 0.85 } : null)}
    >
      <Card style={{ marginBottom: spacing.sm }}>
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
              <IconButton
                testID={`discover-preview-${position}`}
                icon={isThisPreviewPlaying ? Pause : Play}
                size={18}
                onPress={onPreviewPress}
                accessibilityLabel={isThisPreviewPlaying ? 'Pause preview' : 'Play preview'}
              />
            ) : undefined
          }
        >
          <Text variant="bodyStrong" numberOfLines={1}>
            {result.title}
          </Text>
          {secondary !== null ? (
            <Text variant="label" tone="secondary" numberOfLines={1} style={{ marginTop: 2 }}>
              {secondary}
            </Text>
          ) : null}
        </Row>
      </Card>
    </Pressable>
  );
}

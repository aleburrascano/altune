/**
 * DeezerEnrichmentSection — Deezer metadata block on the detail screen.
 *
 * Track: tempo (BPM) and an explicit badge. Album: record label and genre pills.
 * Renders nothing when there is no enrichment, or when the payload carries only
 * non-displayed fields (gain / upc) (docs/providers/deezer.md caps 7–8).
 */

import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme/useTheme';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type {
  DeezerEnrichmentResponse,
  DiscoveryKind,
} from '@shared/api-client/discovery';

const MAX_GENRES = 4;

export function DeezerEnrichmentSection({
  kind,
  enrichment,
}: {
  kind: DiscoveryKind;
  enrichment: DeezerEnrichmentResponse | null;
}): ReactElement | null {
  const theme = useTheme();

  if (enrichment === null) {
    return null;
  }

  const isTrack = kind === 'track';
  const tempo = isTrack && enrichment.bpm > 0 ? `${enrichment.bpm} BPM` : '';
  const explicit = isTrack && enrichment.explicit;
  const label = kind === 'album' ? enrichment.label : '';
  const genres = kind === 'album' ? enrichment.genres.slice(0, MAX_GENRES) : [];

  const hasContent = tempo !== '' || explicit || label !== '' || genres.length > 0;
  if (!hasContent) {
    return null;
  }

  const meta = [tempo, explicit ? 'Explicit' : ''].filter((p) => p !== '');

  return (
    <View testID="detail-deezer" style={styles.section}>
      <Text variant="label" tone="tertiary" style={styles.heading}>
        Deezer
      </Text>

      {meta.length > 0 ? (
        <Text testID="detail-deezer-meta" variant="label" tone="tertiary" style={styles.line}>
          {meta.join('  ·  ')}
        </Text>
      ) : null}

      {label !== '' ? (
        <Text testID="detail-deezer-label" variant="body" tone="secondary" style={styles.line}>
          {label}
        </Text>
      ) : null}

      {genres.length > 0 ? (
        <View style={styles.chips}>
          {genres.map((genre, index) => (
            <View
              key={genre}
              testID={`detail-deezer-genre-${index}`}
              style={[styles.chip, { borderColor: theme.color.border }]}
            >
              <Text variant="caption" tone="secondary">
                {genre}
              </Text>
            </View>
          ))}
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  section: { marginTop: spacing.xl, alignItems: 'center', gap: spacing.sm },
  heading: { textAlign: 'center', textTransform: 'uppercase', letterSpacing: 1 },
  line: { textAlign: 'center' },
  chips: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    justifyContent: 'center',
    gap: spacing.sm,
  },
  chip: {
    borderWidth: 1,
    borderRadius: radius.full,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
  },
});

/**
 * LyricsSection — Deezer lyrics block on the track detail screen.
 *
 * Renders the time-synced lines when available (one line per row), else the
 * plain text, plus the songwriter credits and copyright. Renders nothing when
 * there are no lyrics (docs/providers/deezer.md cap 6). Long-form text is
 * left-aligned for readability, unlike the centered metadata sections.
 */

import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme/tokens';

import type { LyricsResponse } from '@shared/api-client/discovery';

export function LyricsSection({
  lyrics,
}: {
  lyrics: LyricsResponse | null;
}): ReactElement | null {
  if (lyrics === null) {
    return null;
  }

  const lines =
    lyrics.synced_lines.length > 0
      ? lyrics.synced_lines.map((l) => l.line)
      : lyrics.plain.split('\n');

  if (lines.every((line) => line.trim() === '')) {
    return null;
  }

  const writers =
    lyrics.writers.length > 0 ? `Written by ${lyrics.writers.join(', ')}` : '';

  return (
    <View testID="detail-lyrics" style={styles.section}>
      <Text variant="label" tone="tertiary" style={styles.heading}>
        Lyrics
      </Text>

      <View testID="detail-lyrics-body" style={styles.body}>
        {lines.map((line, index) => (
          <Text
            key={index}
            variant="body"
            tone={line.trim() === '' ? 'tertiary' : 'secondary'}
            style={styles.line}
          >
            {line.trim() === '' ? ' ' : line}
          </Text>
        ))}
      </View>

      {writers !== '' ? (
        <Text testID="detail-lyrics-writers" variant="caption" tone="tertiary" style={styles.credit}>
          {writers}
        </Text>
      ) : null}

      {lyrics.copyright !== '' ? (
        <Text variant="caption" tone="tertiary" style={styles.credit}>
          {lyrics.copyright}
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  section: { marginTop: spacing.xl, gap: spacing.sm },
  heading: {
    textAlign: 'center',
    textTransform: 'uppercase',
    letterSpacing: 1,
  },
  body: { gap: spacing.xs },
  line: { textAlign: 'center' },
  credit: { textAlign: 'center', marginTop: spacing.xs },
});

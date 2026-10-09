import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text, type TextTone } from '@shared/ui/primitives/Text';
import { spacing, useTheme } from '@shared/ui/theme';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import { formatDuration } from '@shared/ui/displayFormat';

import { trackExtras } from '@/features/detail/domain/trackExtras';
import { formatRuntime } from '@features/detail/ui/formatters';
import type { TrackDetailActions } from '@features/detail/hooks/useTrackDetailActions';

export type DetailFact = {
  label: string;
  value: string;
  tone?: TextTone;
};

export function DetailFacts({
  facts,
  testID,
}: {
  facts: readonly (DetailFact | null)[];
  testID?: string;
}): ReactElement | null {
  const theme = useTheme();
  const present = facts.filter((fact): fact is DetailFact => fact !== null);

  if (present.length === 0) {
    return null;
  }

  return (
    <View testID={testID} style={[styles.row, { borderTopColor: theme.color.border }]}>
      {present.map((fact) => (
        <View key={fact.label} style={styles.cell}>
          <Text variant="overline" tone="tertiary">
            {fact.label.toUpperCase()}
          </Text>
          <Text variant="bodyStrong" tone={fact.tone ?? 'primary'} style={styles.value}>
            {fact.value}
          </Text>
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingTop: spacing.md,
    marginTop: spacing.xl,
  },
  cell: { flexShrink: 1 },
  value: { marginTop: 1 },
});

export type ArtistFactsInput = {
  releases: number;
  inLibrary: number;
  listeners: string | null;
};

function releasesFact(releases: number): DetailFact | null {
  return releases > 0 ? { label: 'Releases', value: String(releases) } : null;
}

function inLibraryFact(inLibrary: number): DetailFact | null {
  return inLibrary > 0 ? { label: 'In library', value: String(inLibrary) } : null;
}

function listenersFact(listeners: string | null): DetailFact | null {
  return listeners !== null ? { label: 'Listeners', value: listeners } : null;
}

export function buildArtistFacts(input: ArtistFactsInput): (DetailFact | null)[] {
  return [
    releasesFact(input.releases),
    inLibraryFact(input.inLibrary),
    listenersFact(input.listeners),
  ];
}

function tracksFact(tracks: readonly DiscoveryResult[]): DetailFact | null {
  return tracks.length > 0 ? { label: 'Tracks', value: String(tracks.length) } : null;
}

function runtimeSecondsOf(tracks: readonly DiscoveryResult[]): number {
  return tracks.reduce((sum, t) => sum + (trackExtras(t.extras).durationSeconds ?? 0), 0);
}

function runtimeFact(tracks: readonly DiscoveryResult[]): DetailFact | null {
  const runtime = formatRuntime(runtimeSecondsOf(tracks));
  return runtime !== null ? { label: 'Runtime', value: runtime } : null;
}

function releasedFact(year: string | null): DetailFact | null {
  return year !== null ? { label: 'Released', value: year } : null;
}

export function buildAlbumFacts(
  tracks: readonly DiscoveryResult[],
  year: string | null,
): (DetailFact | null)[] {
  return [tracksFact(tracks), runtimeFact(tracks), releasedFact(year)];
}

type FactsInput = Pick<TrackDetailActions, 'durationSeconds' | 'year' | 'source' | 'isPreview'>;

function lengthFact(durationSeconds: number | null): DetailFact | null {
  if (durationSeconds == null || durationSeconds <= 0) return null;
  return { label: 'Length', value: formatDuration(durationSeconds) };
}

function sourceFact(source: FactsInput['source'], isPreview: boolean): DetailFact | null {
  if (source === null) return null;
  if (isPreview) return { label: 'Source', value: 'Preview', tone: 'warning' };
  return { label: 'Source', value: 'Library' };
}

export function buildTrackFacts(input: FactsInput): (DetailFact | null)[] {
  return [
    lengthFact(input.durationSeconds),
    input.year !== null ? { label: 'Released', value: input.year } : null,
    sourceFact(input.source, input.isPreview),
  ];
}

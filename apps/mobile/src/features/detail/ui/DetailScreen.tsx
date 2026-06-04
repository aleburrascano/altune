/**
 * DetailScreen — read-only detail for a tapped discovery result.
 *
 * Fed by the in-memory handoff (no per-item backend fetch). The header (back
 * affordance + hero artwork + title/subtitle/kind) is shared across kinds;
 * the body differs per kind (track info rows + Save; album/artist placeholders)
 * and is filled in by later slices. An empty handoff redirects to /discover.
 *
 * Primitives are imported directly (not via the @shared/ui barrel) so jest
 * component tests don't transitively load unrelated native modules; Artwork's
 * expo-image dependency is mocked in the test.
 */

import { Redirect, useRouter } from 'expo-router';
import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Screen } from '@shared/ui/primitives/Screen';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme/tokens';

import { getDetailHandoff } from '@shared/lib/detail-handoff';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { trackInfoRows } from '../extras';
import { useLateralNav } from '../hooks/useLateralNav';
import { useSaveTrack } from '../hooks/useSaveTrack';
import { toCreateTrackRequest } from '../save-cache';

const HERO_SIZE = 200;

function _kindLabel(kind: 'artist' | 'album' | 'track'): string {
  if (kind === 'artist') {
    return 'Artist';
  }
  return kind === 'album' ? 'Album' : 'Song';
}

export function DetailScreen(): ReactElement {
  const router = useRouter();
  const result = getDetailHandoff();
  const lateralNav = useLateralNav();

  if (result === null) {
    return <Redirect href="/discover" />;
  }

  const isArtist = result.kind === 'artist';
  const canNavToArtist = result.subtitle !== null && result.kind !== 'artist';

  const onArtistPress = (): void => {
    if (canNavToArtist && result.subtitle !== null) {
      void lateralNav.navigateTo(result.subtitle, 'artist');
    }
  };

  return (
    <Screen testID="detail-header">
      <Pressable
        testID="detail-back"
        onPress={() => router.back()}
        style={({ pressed }) => [styles.back, pressed ? { opacity: 0.6 } : null]}
      >
        <Text variant="label" tone="accent">
          ‹ Back
        </Text>
      </Pressable>

      <View style={styles.hero}>
        <Artwork
          uri={result.image_url}
          size={HERO_SIZE}
          radius={isArtist ? radius.full : radius.lg}
          accessibilityLabel={result.title}
        />
        <Text variant="displayL" style={styles.title} numberOfLines={2}>
          {result.title}
        </Text>
        {result.subtitle !== null ? (
          canNavToArtist ? (
            <Pressable
              testID="detail-artist-link"
              onPress={onArtistPress}
              disabled={lateralNav.state === 'searching'}
              style={({ pressed }) => (pressed ? { opacity: 0.6 } : null)}
            >
              <Text variant="body" tone="accent" numberOfLines={1}>
                {result.subtitle}
              </Text>
            </Pressable>
          ) : (
            <Text variant="body" tone="secondary" numberOfLines={1}>
              {result.subtitle}
            </Text>
          )
        ) : null}
        <Text variant="label" tone="tertiary" style={styles.kind}>
          {_kindLabel(result.kind)}
        </Text>
      </View>

      {result.kind === 'track' ? (
        <TrackDetailBody result={result} lateralNav={lateralNav} />
      ) : null}

      {result.kind === 'album' ? (
        <View testID="detail-tracklist-placeholder" style={styles.placeholder}>
          <Text variant="body" tone="tertiary">
            Tracklist coming soon
          </Text>
        </View>
      ) : null}

      {result.kind === 'artist' ? (
        <View testID="detail-discography-placeholder" style={styles.placeholder}>
          <Text variant="body" tone="tertiary">
            Discography coming soon
          </Text>
        </View>
      ) : null}
    </Screen>
  );
}

type LateralNavHandle = {
  navigateTo: (query: string, kind: 'artist' | 'album' | 'track') => Promise<void>;
  state: 'idle' | 'searching';
};

/** Track body: info rows + an optimistic Save-to-library action. */
function TrackDetailBody({
  result,
  lateralNav,
}: {
  result: DiscoveryResult;
  lateralNav: LateralNavHandle;
}): ReactElement {
  const save = useSaveTrack();
  const rows = trackInfoRows(result.extras);
  // AC#9: a Track requires a non-empty artist. When the result has no subtitle
  // (artist), that invariant can't be met — disable Save rather than POST an
  // invalid body.
  const canSave = result.subtitle !== null && result.subtitle.length > 0;

  const onSave = (): void => {
    if (!canSave) {
      return;
    }
    save.mutate(toCreateTrackRequest(result));
  };

  const albumName =
    typeof result.extras['album'] === 'string' && result.extras['album'].length > 0
      ? result.extras['album']
      : null;

  const onAlbumPress = (): void => {
    if (albumName !== null && result.subtitle !== null) {
      // Include artist for disambiguation: "Album Name Artist Name"
      void lateralNav.navigateTo(`${albumName} ${result.subtitle}`, 'album');
    }
  };

  return (
    <View testID="detail-track-info" style={styles.info}>
      {rows.map((row) =>
        row.key === 'album' && albumName !== null ? (
          <Pressable
            key={row.key}
            testID="detail-info-album"
            onPress={onAlbumPress}
            disabled={lateralNav.state === 'searching'}
            style={({ pressed }) => [styles.infoRow, pressed ? { opacity: 0.6 } : null]}
          >
            <Text variant="label" tone="tertiary">
              {row.label}
            </Text>
            <Text variant="body" tone="accent">
              {row.value}
            </Text>
          </Pressable>
        ) : (
          <View key={row.key} testID={`detail-info-${row.key}`} style={styles.infoRow}>
            <Text variant="label" tone="tertiary">
              {row.label}
            </Text>
            <Text variant="body">{row.value}</Text>
          </View>
        ),
      )}
      <Button
        testID="detail-save"
        label={save.isSuccess ? 'Saved' : 'Save to Library'}
        onPress={onSave}
        disabled={!canSave || save.isPending || save.isSuccess}
        loading={save.isPending}
        haptic
        style={styles.save}
      />
      {save.isError ? (
        <Banner testID="detail-save-error" tone="danger">
          Couldn’t save this track. Try again.
        </Banner>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  back: { paddingVertical: spacing.md, alignSelf: 'flex-start' },
  hero: { alignItems: 'center', paddingTop: spacing.lg, gap: spacing.sm },
  title: { textAlign: 'center', marginTop: spacing.lg },
  kind: { marginTop: spacing.xs },
  info: { marginTop: spacing['2xl'], gap: spacing.md },
  infoRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    gap: spacing.lg,
  },
  placeholder: { marginTop: spacing['2xl'], alignItems: 'center' },
  save: { marginTop: spacing.lg },
});

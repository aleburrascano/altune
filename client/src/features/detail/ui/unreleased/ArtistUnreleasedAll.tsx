import { Redirect, useLocalSearchParams } from 'expo-router';
import type { ReactElement } from 'react';
import { FlatList, StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';
import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedTrack } from '@shared/apiClient/enrichment';
import { readDetailHandoff } from '@shared/navigation/detailHandoff';

import { unreleasedView, type UnreleasedWhere } from '@features/detail/domain/unreleasedView';
import { DetailHandoffProvider } from '@features/detail/hooks/useDetailHandoff';
import { useUnreleasedTracks } from '@features/detail/hooks/useUnreleasedTracks';
import { DETAIL_GUTTER } from '@features/detail/ui/detailStyles';
import { UnreleasedRow } from '@features/detail/ui/unreleased/ArtistUnreleased';

type ArtistProps = { artist: DiscoveryResult };
type ListProps = ArtistProps & { tracks: UnreleasedTrack[] };
type ScreenParams = { handoff?: string; where?: string };

const SCREEN_TITLE = 'Not on streaming';
const WHERES: readonly UnreleasedWhere[] = ['all', 'released', 'unreleased'];

function trackKey(track: UnreleasedTrack): string {
  return track.key;
}

function isWhere(value: string | undefined): value is UnreleasedWhere {
  return WHERES.some((where) => where === value);
}

function railFirstChipWhere(tracks: UnreleasedTrack[]): UnreleasedWhere {
  return tracks.some((t) => t.off_streaming) ? 'released' : 'unreleased';
}

function rowRenderer(artist: DiscoveryResult) {
  return function renderRow({ item }: { item: UnreleasedTrack }): ReactElement {
    return (
      <View style={styles.row}>
        <UnreleasedRow artist={artist} track={item} />
      </View>
    );
  };
}

function UnreleasedList({ artist, tracks }: ListProps): ReactElement {
  return (
    <FlatList
      testID="detail-unreleased-all-list"
      data={tracks}
      keyExtractor={trackKey}
      renderItem={rowRenderer(artist)}
      initialNumToRender={tracks.length}
      contentContainerStyle={styles.content}
    />
  );
}

function UnreleasedAllBody({ artist }: ArtistProps): ReactElement {
  const params = useLocalSearchParams<ScreenParams>();
  const { tracks } = useUnreleasedTracks(artist);
  const where = isWhere(params.where) ? params.where : railFirstChipWhere(tracks);
  const shown = unreleasedView(tracks, 'popular', where, 'all');
  return (
    <View style={styles.screen} testID="detail-unreleased-all">
      <Text variant="title" style={styles.title}>
        {SCREEN_TITLE}
      </Text>
      <UnreleasedList artist={artist} tracks={shown} />
    </View>
  );
}

export function UnreleasedAllScreen(): ReactElement {
  const params = useLocalSearchParams<ScreenParams>();
  const handoff = readDetailHandoff(params.handoff);
  if (handoff === null) return <Redirect href="/discover" />;
  return (
    <DetailHandoffProvider value={handoff}>
      <UnreleasedAllBody artist={handoff.result} />
    </DetailHandoffProvider>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  title: { paddingHorizontal: DETAIL_GUTTER, paddingVertical: spacing.md },
  content: { paddingHorizontal: DETAIL_GUTTER, gap: spacing.md },
  row: { alignItems: 'flex-start' },
});

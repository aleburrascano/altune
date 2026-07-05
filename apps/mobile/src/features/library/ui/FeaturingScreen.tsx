import { useLocalSearchParams, useRouter, useSegments } from 'expo-router';
import { useMemo, useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';
import { ChevronLeft } from 'lucide-react-native';

import { searchDiscovery } from '@shared/api-client/discovery';
import type { FeaturedArtist, TrackResponse } from '@shared/api-client/types';
import { setDetailHandoff } from '@shared/lib/detail-handoff';
import { trackToDiscoveryResult } from '@shared/lib/track-to-discovery';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import { buildPlayableQueue } from '@shared/playback/playFromList';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Button, Screen, Skeleton, Text, spacing } from '@shared/ui';
import { ContextMenu, type ContextMenuItem } from '@shared/ui/primitives/ContextMenu';
import { IconButton } from '@shared/ui/primitives/IconButton';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import { useDeleteTrack } from '../hooks/useDeleteTrack';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { useTracksFeaturing } from '../hooks/useTracksFeaturing';
import { SongsList } from './SongsList';

export function FeaturingScreen(): ReactElement {
  const params = useLocalSearchParams<{ name?: string; mbid?: string; deezer_id?: string }>();
  const router = useRouter();
  const segments = useSegments();
  const tabRoot = segments[1] === 'discover' ? 'discover' : 'library';

  const fa: FeaturedArtist = useMemo(
    () => ({
      name: params.name ?? '',
      mbid: params.mbid && params.mbid.length > 0 ? params.mbid : null,
      deezer_id: params.deezer_id ? Number(params.deezer_id) : null,
    }),
    [params.name, params.mbid, params.deezer_id],
  );

  const { data, isLoading, isError, refetch } = useTracksFeaturing(fa);
  const deleteMutation = useDeleteTrack();
  const retryMutation = useRetryAcquisition();
  const playback = usePlayback();
  const queue = useQueuePlayback();

  const [action, setAction] = useState<{ track: TrackResponse; anchor: MenuAnchor } | null>(null);
  const [exploring, setExploring] = useState(false);

  const goBack = () => (router.canGoBack() ? router.back() : router.replace('/library'));
  const tracks = data?.items ?? [];
  const retryingTrackId = retryMutation.isPending ? retryMutation.variables : undefined;

  // Navigate within the current tab stack (discover or library) so back returns
  // to the detail screen we came from, not the tab root.
  const openTrackDetail = (track: TrackResponse): void => {
    setDetailHandoff(trackToDiscoveryResult(track));
    router.push(`/${tabRoot}/detail` as '/discover/detail');
  };

  // Option A: when the library has nothing featuring this artist, don't dead-end —
  // search discovery for them and open their detail so tapping always leads somewhere.
  const exploreArtist = async (): Promise<void> => {
    if (exploring) return;
    setExploring(true);
    try {
      const res = await searchDiscovery({ q: fa.name, kinds: ['artist', 'track'], limit: 1, saveHistory: false });
      const result = res.results[0];
      if (result !== undefined) {
        setDetailHandoff(result);
        router.push(`/${tabRoot}/detail` as '/discover/detail');
      }
    } finally {
      setExploring(false);
    }
  };

  const trackMenuItems = (track: TrackResponse): ContextMenuItem[] => {
    const ready = track.acquisition_status === 'ready';
    return [
      ...(ready
        ? [
            { label: 'Play Next', onPress: () => queue.playNext(toPlaybackTrack(track)) },
            { label: 'Add to Queue', onPress: () => queue.addToQueue(toPlaybackTrack(track)) },
          ]
        : []),
      { label: 'View Details', onPress: () => openTrackDetail(track) },
      { label: 'Remove from Library', tone: 'danger', onPress: () => deleteMutation.mutate(track.id) },
    ];
  };

  return (
    <Screen>
      <View style={styles.header}>
        <IconButton icon={ChevronLeft} size={24} onPress={goBack} accessibilityLabel="Back" />
        <View style={styles.headerText}>
          <Text variant="caption" tone="tertiary">Featuring</Text>
          <Text variant="title" numberOfLines={1}>{fa.name}</Text>
        </View>
      </View>

      {isLoading ? (
        <View style={styles.list}>
          <Skeleton height={56} />
          <Skeleton height={56} />
          <Skeleton height={56} />
        </View>
      ) : isError ? (
        <View style={styles.centered}>
          <Text variant="body" tone="secondary">Couldn't load tracks.</Text>
          <Text variant="label" tone="accent" onPress={() => void refetch()} style={styles.retry}>
            Retry
          </Text>
        </View>
      ) : tracks.length === 0 ? (
        <View style={styles.centered}>
          <Text variant="body" tone="secondary" style={styles.emptyText}>
            Nothing in your library featuring {fa.name} yet.
          </Text>
          <Button
            testID="featuring-explore"
            label={exploring ? 'Searching…' : `Search for ${fa.name}`}
            variant="ghost"
            loading={exploring}
            onPress={() => void exploreArtist()}
          />
        </View>
      ) : (
        <SongsList
          tracks={tracks}
          emptyLabel=""
          onPlay={(track) => {
            const { playable, startIndex } = buildPlayableQueue(tracks, track.id);
            queue.playFromList(playable, startIndex, { kind: 'library' });
          }}
          onPress={openTrackDetail}
          onMore={(track, anchor) => setAction({ track, anchor })}
          onRetry={(track) => retryMutation.mutate(track.id)}
          retryingTrackId={retryingTrackId}
          isPlaying={(id) => isCurrentlyPlaying(playback, { kind: 'library', trackId: id })}
        />
      )}

      <ContextMenu
        visible={action != null}
        anchor={action?.anchor}
        items={action != null ? trackMenuItems(action.track) : []}
        onClose={() => setAction(null)}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    paddingBottom: spacing.md,
  },
  headerText: { flex: 1 },
  list: { gap: spacing.sm, paddingTop: spacing.md },
  centered: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.md, padding: spacing.xl },
  emptyText: { textAlign: 'center' },
  retry: { paddingTop: spacing.sm },
});

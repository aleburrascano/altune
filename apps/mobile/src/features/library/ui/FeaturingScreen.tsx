import { useLocalSearchParams, useRouter } from 'expo-router';
import { useMemo, useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';
import { ChevronLeft } from 'lucide-react-native';

import type { FeaturedArtist, TrackResponse } from '@shared/api-client/types';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import { buildPlayableQueue } from '@shared/playback/playFromList';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Screen, Skeleton, Text, spacing } from '@shared/ui';
import { ContextMenu, type ContextMenuItem } from '@shared/ui/primitives/ContextMenu';
import { IconButton } from '@shared/ui/primitives/IconButton';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import { useDeleteTrack } from '../hooks/useDeleteTrack';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { useTracksFeaturing } from '../hooks/useTracksFeaturing';
import { SongsList } from './SongsList';
import { useLibraryNavigation } from './useLibraryNavigation';

export function FeaturingScreen(): ReactElement {
  const params = useLocalSearchParams<{ name?: string; mbid?: string; deezer_id?: string }>();
  const router = useRouter();

  const fa: FeaturedArtist = useMemo(
    () => ({
      name: params.name ?? '',
      mbid: params.mbid && params.mbid.length > 0 ? params.mbid : null,
      deezer_id: params.deezer_id ? Number(params.deezer_id) : null,
    }),
    [params.name, params.mbid, params.deezer_id],
  );

  const { data, isLoading, isError, refetch } = useTracksFeaturing(fa);
  const { navigateToTrack } = useLibraryNavigation(router);
  const deleteMutation = useDeleteTrack();
  const retryMutation = useRetryAcquisition();
  const playback = usePlayback();
  const queue = useQueuePlayback();

  const [action, setAction] = useState<{ track: TrackResponse; anchor: MenuAnchor } | null>(null);

  const goBack = () => (router.canGoBack() ? router.back() : router.replace('/library'));
  const tracks = data?.items ?? [];
  const retryingTrackId = retryMutation.isPending ? retryMutation.variables : undefined;

  const trackMenuItems = (track: TrackResponse): ContextMenuItem[] => {
    const ready = track.acquisition_status === 'ready';
    return [
      ...(ready
        ? [
            { label: 'Play Next', onPress: () => queue.playNext(toPlaybackTrack(track)) },
            { label: 'Add to Queue', onPress: () => queue.addToQueue(toPlaybackTrack(track)) },
          ]
        : []),
      { label: 'View Details', onPress: () => navigateToTrack(track) },
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
      ) : (
        <SongsList
          tracks={tracks}
          emptyLabel={`No tracks in your library featuring ${fa.name} yet.`}
          onPlay={(track) => {
            const { playable, startIndex } = buildPlayableQueue(tracks, track.id);
            queue.playFromList(playable, startIndex, { kind: 'library' });
          }}
          onPress={navigateToTrack}
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
  centered: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.sm },
  retry: { paddingTop: spacing.sm },
});

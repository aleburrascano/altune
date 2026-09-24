import { useLocalSearchParams, useRouter } from 'expo-router';
import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { NO_PLAYLIST_ID, parsePlaylistId, type PlaylistId } from '@shared/api-client/ids';
import { Screen, Skeleton, spacing } from '@shared/ui';
import type { PlaylistDetailResponse } from '@shared/api-client/types';

import { goBackOrToLibrary } from '../goBackOrToLibrary';
import { useLoggedPlaylistDetailFailure } from '../hooks/useLoggedPlaylistDetailFailure';
import { usePlaylistDetail } from '../hooks/usePlaylistDetail';
import { BackHeader } from './BackHeader';
import { PlaylistDetailContent, type ContentProps } from './PlaylistDetailContent';
import { PlaylistDetailFailure } from './PlaylistDetailFailure';

type Router = ReturnType<typeof useRouter>;

type ScreenQuery = {
  playlist: PlaylistDetailResponse | undefined;
  isLoading: boolean;
  isRefetching: boolean;
  error: Error | null;
  refetch: () => void;
};

type StatesProps = { router: Router; playlistId: PlaylistId; state: ScreenQuery };

function HeroSkeleton(): ReactElement {
  return (
    <View style={styles.heroLoading}>
      <Skeleton width={160} height={160} radius={8} />
      <Skeleton width={200} height={20} />
      <Skeleton width={100} height={14} />
    </View>
  );
}

function PlaylistDetailLoading({ onBack }: { onBack: () => void }): ReactElement {
  return (
    <Screen>
      <BackHeader onBack={onBack} />
      <HeroSkeleton />
    </Screen>
  );
}

function useRouteParamPlaylistId(): PlaylistId {
  const params = useLocalSearchParams<{ id: string }>();
  const parsedId = parsePlaylistId(params.id ?? '');
  return parsedId.ok ? parsedId.id : NO_PLAYLIST_ID;
}

function usePlaylistScreenQuery(playlistId: PlaylistId): ScreenQuery {
  const { data: playlist, isLoading, isRefetching, error, refetch } = usePlaylistDetail(playlistId);
  useLoggedPlaylistDetailFailure(playlistId, error);
  return { playlist, isLoading, isRefetching, error, refetch: () => void refetch() };
}

function LibraryRedirect({ router }: { router: Router }): ReactElement {
  router.replace('/library');
  return (
    <Screen>
      <View />
    </Screen>
  );
}

type FailedProps = { state: ScreenQuery; onBack: () => void; onLibrary: () => void };

function PlaylistDetailFailedView({ state, onBack, onLibrary }: FailedProps): ReactElement {
  return (
    <Screen>
      <BackHeader onBack={onBack} />
      <PlaylistDetailFailure
        error={state.error}
        onRetry={state.refetch}
        onGoToLibrary={onLibrary}
      />
    </Screen>
  );
}

function loadedProps(props: StatesProps, playlist: PlaylistDetailResponse): ContentProps {
  const { router, playlistId, state } = props;
  return { playlistId, playlist, router, refreshing: state.isRefetching, onRefresh: state.refetch };
}

function PlaylistDetailStates(props: StatesProps): ReactElement {
  const { playlist, error, isLoading } = props.state;
  const goBack = (): void => goBackOrToLibrary(props.router);
  const onLibrary = (): void => props.router.replace('/library');
  if (isLoading) return <PlaylistDetailLoading onBack={goBack} />;
  if (error || !playlist) {
    return <PlaylistDetailFailedView state={props.state} onBack={goBack} onLibrary={onLibrary} />;
  }
  return <PlaylistDetailContent {...loadedProps(props, playlist)} />;
}

export function PlaylistDetailScreen(): ReactElement {
  const router = useRouter();
  const playlistId = useRouteParamPlaylistId();
  const state = usePlaylistScreenQuery(playlistId);
  if (!playlistId) return <LibraryRedirect router={router} />;
  return <PlaylistDetailStates router={router} playlistId={playlistId} state={state} />;
}

const styles = StyleSheet.create({
  heroLoading: {
    alignItems: 'center',
    gap: spacing.sm,
    paddingBottom: spacing.xl,
  },
});

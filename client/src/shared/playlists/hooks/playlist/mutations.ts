import { useQueryClient, type QueryClient } from '@tanstack/react-query';

import { ApiError, isServerFaultStatus } from '@shared/failure/errors';
import type { PlaylistId, TrackId } from '@shared/apiClient/ids';
import type { PlaylistDetailResponse, PlaylistResponse } from '@shared/apiClient/playlistResponse';
import { RETRY_TAIL } from '@shared/failure/describeError';
import { countLabel } from '@shared/ui/format/displayFormat';
import { playlistKeys } from '@shared/query/queryKeys';
import { useAppMutation, useOptimisticMutation } from '@shared/query/useAppMutation';
import { currentSessionEpoch, isSameSession } from '@shared/session/signOutCleanup';
import { guardedMutationOptions } from '@shared/session/guardedMutation';
import { showFailureAlert } from '@shared/ui';

import { usePlaylistsPort } from '@shared/playlists/ports/PlaylistsPortProvider';
import type { PlaylistsPort } from '@shared/playlists/ports/playlistsPort';

type AddTracksVariables = { playlistId: PlaylistId; trackIds: TrackId[] };
type CreateWithTracksVariables = { name: string; trackIds: TrackId[] };
type PlaylistList = { items: PlaylistResponse[] };
type PlaylistDetail = { name: string; tracks: { id: TrackId }[] };
type AlertContent = { title: string; message: string };
type StartedIn = { epoch: number } | undefined;
type CreateSettled = PlaylistResponse | CreateWithTracksResult | undefined;
type CreateVariables = string | CreateWithTracksVariables;
type CreatedInSession = { playlist: PlaylistResponse; trackIds: TrackId[]; epoch: number };

type CreateWithTracksResult =
  | { playlist: PlaylistResponse; added: number; addFailed: false }
  | { playlist?: PlaylistResponse; added: 0; addFailed: true };

function alreadyThereMessage(skipped: number, playlistName: string | undefined): string {
  const where = playlistName != null ? `already in ${playlistName}` : 'already in the playlist';
  return skipped === 1 ? `One track was ${where}.` : `${skipped} tracks were ${where}.`;
}

function orphanedPlaylistMessage(requested: number): string {
  return requested === 1
    ? 'Playlist created, but the track could not be added. Try adding it manually.'
    : 'Playlist created, but the tracks could not be added. Try adding them manually.';
}

async function rollBackCreatedPlaylist(
  port: PlaylistsPort,
  playlist: PlaylistResponse,
): Promise<CreateWithTracksResult> {
  try {
    await port.deletePlaylist(playlist.id);
    return { added: 0, addFailed: true };
  } catch {
    return { playlist, added: 0, addFailed: true };
  }
}

function leftInPlace(playlist: PlaylistResponse): CreateWithTracksResult {
  return { playlist, added: 0, addFailed: true };
}

function isDefiniteRefusal(error: unknown): boolean {
  return error instanceof ApiError && !isServerFaultStatus(error.status);
}

function requestedTracksPresent(detail: PlaylistDetailResponse, trackIds: TrackId[]): number {
  const present = new Set(detail.tracks.map((track) => track.id));
  return new Set(trackIds.filter((id) => present.has(id))).size;
}

function resultFromPresent(playlist: PlaylistResponse, added: number): CreateWithTracksResult {
  return added > 0 ? { playlist, added, addFailed: false } : leftInPlace(playlist);
}

function rereadAfterUnclearAdd(
  port: PlaylistsPort,
  { playlist, trackIds }: CreatedInSession,
): Promise<CreateWithTracksResult> {
  return port
    .getPlaylist(playlist.id)
    .then((detail) => resultFromPresent(playlist, requestedTracksPresent(detail, trackIds)))
    .catch(() => leftInPlace(playlist));
}

function settleFailedAdd(port: PlaylistsPort, created: CreatedInSession, error: unknown) {
  if (!isSameSession(created.epoch)) return leftInPlace(created.playlist);
  return isDefiniteRefusal(error)
    ? rollBackCreatedPlaylist(port, created.playlist)
    : rereadAfterUnclearAdd(port, created);
}

async function addCreatedTracks(
  port: PlaylistsPort,
  { playlist, trackIds }: CreatedInSession,
): Promise<CreateWithTracksResult> {
  const { added } = await port.addTracksToPlaylist(playlist.id, { track_ids: trackIds });
  return { playlist, added, addFailed: false };
}

async function fillCreatedPlaylist(
  port: PlaylistsPort,
  created: CreatedInSession,
): Promise<CreateWithTracksResult> {
  if (!isSameSession(created.epoch)) return leftInPlace(created.playlist);
  return addCreatedTracks(port, created).catch((error: unknown) =>
    settleFailedAdd(port, created, error),
  );
}

async function createWithTracks(
  port: PlaylistsPort,
  { name, trackIds }: CreateWithTracksVariables,
): Promise<CreateWithTracksResult> {
  const epoch = currentSessionEpoch();
  const playlist = await port.createPlaylist({ name });
  return fillCreatedPlaylist(port, { playlist, trackIds, epoch });
}

function alertCreateFailed(): void {
  showFailureAlert({
    surface: 'playlist.create',
    title: 'Error',
    message: `Could not create the playlist. ${RETRY_TAIL}`,
  });
}

function alertCreatedWithTracks(
  created: CreateWithTracksResult,
  { trackIds }: CreateWithTracksVariables,
) {
  const alert = createWithTracksAlert(created, trackIds.length);
  if (alert !== null) showFailureAlert({ surface: 'playlist.create_with_tracks', ...alert });
}

function refreshPlaylistsInSameSession(queryClient: QueryClient) {
  return (
    _settled: CreateSettled,
    _error: Error | null,
    _variables: CreateVariables,
    startedIn: StartedIn,
  ) =>
    isSameSession(startedIn?.epoch)
      ? queryClient.invalidateQueries({ queryKey: playlistKeys.list })
      : undefined;
}

function createWithTracksAlert(
  result: CreateWithTracksResult,
  requested: number,
): AlertContent | null {
  if (result.addFailed) {
    return result.playlist === undefined
      ? { title: 'Error', message: `Could not create the playlist. ${RETRY_TAIL}` }
      : { title: 'Note', message: orphanedPlaylistMessage(requested) };
  }
  const skipped = requested - result.added;
  if (skipped <= 0) return null;
  return { title: 'Note', message: alreadyThereMessage(skipped, result.playlist.name) };
}

function reinsertTracks<T extends { id: TrackId }>(
  current: T[],
  previous: T[],
  removed: ReadonlySet<TrackId>,
): T[] {
  const present = new Set(current.map((t) => t.id));
  const tracks = [...current];
  previous.forEach((track, index) => {
    if (removed.has(track.id) && !present.has(track.id)) tracks.splice(index, 0, track);
  });
  return tracks;
}

export function useCreatePlaylist() {
  const port = usePlaylistsPort();
  const queryClient = useQueryClient();
  return useAppMutation({
    action: 'playlist.create',
    ...guardedMutationOptions({
      mutationFn: (name: string) => port.createPlaylist({ name }),
      onError: alertCreateFailed,
    }),
    onSettled: refreshPlaylistsInSameSession(queryClient),
  });
}

export function useCreatePlaylistWithTracks() {
  const port = usePlaylistsPort();
  const queryClient = useQueryClient();
  return useAppMutation({
    action: 'playlist.create_with_tracks',
    ...guardedMutationOptions({
      mutationFn: (variables: CreateWithTracksVariables) => createWithTracks(port, variables),
      onSuccess: alertCreatedWithTracks,
      onError: alertCreateFailed,
    }),
    onSettled: refreshPlaylistsInSameSession(queryClient),
  });
}

export function useAddTracksToPlaylist() {
  const port = usePlaylistsPort();
  const queryClient = useQueryClient();
  return useOptimisticMutation({
    action: 'playlist.add_tracks',
    queryKey: playlistKeys.list,
    mutationFn: ({ playlistId, trackIds }: AddTracksVariables) =>
      port.addTracksToPlaylist(playlistId, { track_ids: trackIds }),
    applyOptimistic: (previous: PlaylistList, { playlistId, trackIds }) => ({
      ...previous,
      items: previous.items.map((p) =>
        p.id === playlistId ? { ...p, track_count: p.track_count + trackIds.length } : p,
      ),
    }),
    revertOptimistic: (current, { playlistId, trackIds }, previous) => {
      const before = previous.items.find((p) => p.id === playlistId)?.track_count;
      if (before === undefined) return current;
      const bumped = before + trackIds.length;
      return {
        ...current,
        items: current.items.map((p) =>
          p.id === playlistId && p.track_count === bumped ? { ...p, track_count: before } : p,
        ),
      };
    },
    onSuccess: (outcome, { playlistId, trackIds }) => {
      if (outcome.added < trackIds.length) {
        const name = queryClient
          .getQueryData<PlaylistList>(playlistKeys.list)
          ?.items.find((p) => p.id === playlistId)?.name;
        showFailureAlert({
          surface: 'playlist.add_tracks.already_there',
          title: 'Note',
          message: alreadyThereMessage(trackIds.length - outcome.added, name),
        });
      }
    },
    alertOnError: ({ trackIds }) => ({
      title: 'Add failed',
      message: `Could not add the ${countLabel(trackIds.length, 'track')} to the playlist. ${RETRY_TAIL}`,
    }),
    invalidate: ({ playlistId }) => [playlistKeys.list, playlistKeys.detail(playlistId)],
  });
}

export function useRenamePlaylist(playlistId: PlaylistId) {
  const port = usePlaylistsPort();
  return useOptimisticMutation({
    action: 'playlist.rename',
    queryKey: playlistKeys.detail(playlistId),
    mutationFn: (name: string) => port.renamePlaylist(playlistId, name),
    applyOptimistic: (previous: PlaylistDetail, name) => ({ ...previous, name }),
    revertOptimistic: (current, name, previous) =>
      current.name === name ? { ...current, name: previous.name } : current,
    alertOnError: () => ({
      title: 'Rename failed',
      message: `Could not rename the playlist. ${RETRY_TAIL}`,
    }),
    invalidate: () => [playlistKeys.detail(playlistId), playlistKeys.list],
  });
}

function forgetDeletedPlaylist(queryClient: QueryClient, playlistId: PlaylistId) {
  return (): void => {
    void queryClient.invalidateQueries({ queryKey: playlistKeys.list });
    void queryClient.invalidateQueries({ queryKey: playlistKeys.detail(playlistId) });
  };
}

function alertDeleteFailed(): void {
  showFailureAlert({
    surface: 'playlist.delete',
    title: 'Delete failed',
    message: `Could not delete the playlist. ${RETRY_TAIL}`,
  });
}

export function useDeletePlaylist(playlistId: PlaylistId) {
  const port = usePlaylistsPort();
  const queryClient = useQueryClient();
  return useAppMutation({
    action: 'playlist.delete',
    ...guardedMutationOptions({
      mutationFn: () => port.deletePlaylist(playlistId),
      onSuccess: forgetDeletedPlaylist(queryClient, playlistId),
      onError: alertDeleteFailed,
    }),
  });
}

export function useRemoveTracksFromPlaylist(playlistId: PlaylistId) {
  const port = usePlaylistsPort();
  return useOptimisticMutation({
    action: 'playlist.remove_tracks',
    queryKey: playlistKeys.detail(playlistId),
    mutationFn: (trackIds: TrackId[]) =>
      port.removeTracksFromPlaylist(playlistId, { track_ids: trackIds }),
    applyOptimistic: (previous: PlaylistDetail, trackIds) => {
      const removing = new Set(trackIds);
      return { ...previous, tracks: previous.tracks.filter((t) => !removing.has(t.id)) };
    },
    revertOptimistic: (current, trackIds, previous) => ({
      ...current,
      tracks: reinsertTracks(current.tracks, previous.tracks, new Set(trackIds)),
    }),
    alertOnError: (trackIds) => ({
      title: 'Remove failed',
      message: `Could not remove the ${countLabel(trackIds.length, 'track')}. ${RETRY_TAIL}`,
    }),
    invalidate: () => [playlistKeys.detail(playlistId), playlistKeys.list],
  });
}

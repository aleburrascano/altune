import { useCallback, useState, type Dispatch, type SetStateAction } from 'react';
import { useRouter, type Href } from 'expo-router';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { TrackId } from '@shared/api-client/ids';
import type { FeaturedArtist } from '@shared/api-client/types';
import type { PlaybackSource } from '@shared/playback/types';

import { usePlayback } from '@shared/playback/usePlayback';

import { resolveFeatured } from '../featured-artists';
import { trackExtras } from '../extras-accessors';
import { useDetailHandoff } from '../handoff-context';
import { type LateralNavHandle } from './useLateralNav';
import { useOwnedTrack, type OwnedTrack } from './useOwnedTrack';
import { useReportWrongAlbum } from './useReportWrongAlbum';
import { useRetryTrack } from './useRetryTrack';
import { useSaveTrack, type SaveFailure } from './useSaveTrack';
import { featuringRouteFor, type DetailRoute } from '../navigation';
import { isResultPlaying, resolvePlaySource } from '../play-source';
import { toCreateTrackRequest } from '../save-cache';
import { ownedRetryTrackId, saveControlState, type SaveControlState } from '../save-control-state';

export type { LateralNavHandle };

type SaveState = SaveControlState | 'disabled';

function releasedYear(mbYear: number | undefined, extrasYear: number | null): string | null {
  if (mbYear != null && mbYear > 0) return String(mbYear);
  return extrasYear != null ? String(extrasYear) : null;
}

function failureState(failure: SaveFailure): SaveControlState {
  return failure.isRetryable ? 'failed' : 'rejected';
}

// A failure the status store remembers — an earlier attempt, or an acquisition
// the server failed — carries a reason but no classification, and the user has
// always been free to re-attempt it.
function rememberedFailure(owned: OwnedTrack | null): SaveFailure | null {
  if (owned?.acquisitionStatus !== 'failed' || owned.failureMessage === null) {
    return null;
  }
  return { message: owned.failureMessage, isRetryable: true };
}

export type TrackDetailActions = {
  albumName: string | null;
  featured: FeaturedArtist[];
  durationSeconds: number | null;
  year: string | null;
  source: PlaybackSource | null;
  isPreview: boolean;
  playing: boolean;
  playLabel: string;
  playLoading: boolean;
  onTogglePlay: () => void;
  canSave: boolean;
  saveFailure: SaveFailure | null;
  saveState: SaveState;
  saveInteractive: boolean;
  saveDisplayState: SaveControlState;
  onSave: () => void;
  resolveTrackIds: () => Promise<TrackId[]>;
  playlistSheetVisible: boolean;
  setPlaylistSheetVisible: Dispatch<SetStateAction<boolean>>;
  wrongAlbumReported: boolean;
  onReportWrongAlbum: () => void;
  onAlbumPress: () => void;
  onFeaturedPress: (featuredArtist: FeaturedArtist) => void;
};

export function useTrackDetailActions({
  result,
  lateralNav,
  detailRoute,
  deezerFeatured,
  mbYear,
}: {
  result: DiscoveryResult;
  lateralNav: LateralNavHandle;
  detailRoute: DetailRoute;
  deezerFeatured?: FeaturedArtist[] | undefined;
  mbYear?: number | undefined;
}): TrackDetailActions {
  const router = useRouter();
  const save = useSaveTrack();
  const retry = useRetryTrack();
  const searchId = useDetailHandoff()?.searchId;
  const [playlistSheetVisible, setPlaylistSheetVisible] = useState(false);
  const wrongAlbum = useReportWrongAlbum(result);
  const playback = usePlayback();
  const te = trackExtras(result.extras);
  const owned = useOwnedTrack(te, { title: result.title, artist: result.subtitle });

  const canSave = (result.subtitle ?? '').length > 0;
  const albumName = te.album;
  const featured = resolveFeatured(result.extras, deezerFeatured, result.title, result.subtitle);

  const source = resolvePlaySource(te, owned);
  const playing = isResultPlaying(playback, te, owned);
  const isPreview = source?.kind === 'preview';

  const saveState: SaveState = !canSave
    ? 'disabled'
    : save.failure !== null
      ? failureState(save.failure)
      : save.isPending
        ? 'saving'
        : saveControlState(owned);
  const saveFailure = save.failure ?? (saveState === 'failed' ? rememberedFailure(owned) : null);
  const saveInteractive = saveState === 'add' || saveState === 'failed';
  const saveDisplayState: SaveControlState = saveState === 'disabled' ? 'add' : saveState;

  const year = releasedYear(mbYear, te.year);

  const onTogglePlay = (): void => {
    if (playing) {
      playback.pause();
      return;
    }
    if (source === null) {
      return;
    }
    void playback.play({
      source,
      title: result.title,
      artist: result.subtitle ?? '',
      artworkUrl: result.image_url,
      durationSeconds: te.durationSeconds ?? undefined,
      searchId: searchId ?? undefined,
      resultSignature: result.result_signature ?? undefined,
    });
  };

  const onSave = (): void => {
    if (!saveInteractive) {
      return;
    }
    const retryId = ownedRetryTrackId(owned);
    if (retryId !== null) {
      retry.mutate(retryId);
      return;
    }
    save.mutate(toCreateTrackRequest(result));
  };

  const resolveTrackIds = useCallback(async (): Promise<TrackId[]> => {
    if (owned !== null) {
      return [owned.trackId];
    }
    const saved = await save.mutateAsync(toCreateTrackRequest(result));
    return [saved.id];
  }, [owned, result, save]);

  const onAlbumPress = (): void => {
    if (result.subtitle !== null && albumName !== null) {
      void lateralNav.navigateTo(`${albumName} ${result.subtitle}`, 'album');
    }
  };

  const onFeaturedPress = (f: FeaturedArtist): void => {
    const href: Href = {
      pathname: featuringRouteFor(detailRoute),
      params: {
        name: f.name,
        ...(f.mbid ? { mbid: f.mbid } : {}),
        ...(f.deezer_id != null ? { deezer_id: String(f.deezer_id) } : {}),
      },
    };
    router.push(href);
  };

  const playLabel = playing ? 'Pause' : isPreview ? 'Play preview' : 'Play';

  return {
    albumName,
    featured,
    durationSeconds: te.durationSeconds,
    year,
    source,
    isPreview,
    playing,
    playLabel,
    playLoading: playback.status === 'loading',
    onTogglePlay,
    canSave,
    saveFailure,
    saveState,
    saveInteractive,
    saveDisplayState,
    onSave,
    resolveTrackIds,
    playlistSheetVisible,
    setPlaylistSheetVisible,
    wrongAlbumReported: wrongAlbum.reported,
    onReportWrongAlbum: wrongAlbum.report,
    onAlbumPress,
    onFeaturedPress,
  };
}

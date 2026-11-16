import { useCallback, useState, type Dispatch, type SetStateAction } from 'react';
import type { DiscoveryResult, TrackId, FeaturedArtist } from '@shared/catalog';
import type { PlaybackSource } from '@shared/playback';

import { usePlayback } from '@shared/playback';

import { resolveFeatured, trackExtras } from '@features/detail/domain/trackExtras';
import { useDetailHandoff } from '@features/detail/view/useDetailHandoff';
import {
  type LateralNavHandle,
  useOpenFeaturing,
} from '@features/detail/hooks/useDetailNavigation';
import { useOwnedTrack } from '@features/detail/hooks/useOwnedTrack';
import { useReportWrongAlbum } from '@features/detail/view/useReportWrongAlbum';
import { useSaveTrack, useTrackSave, type TrackSave } from '@features/detail/view/useSaveTrack';
import { type DetailRoute } from '@features/detail/domain/detailNavigation';
import { isResultPlaying, resolvePlaySource } from '@features/detail/domain/playSource';
import { isOptimisticTrackId, toCreateTrackRequest } from '@features/detail/domain/saveTrack';

export type { LateralNavHandle };

function releasedYear(mbYear: number | undefined, extrasYear: number | null): string | null {
  if (mbYear != null && mbYear > 0) return String(mbYear);
  return extrasYear != null ? String(extrasYear) : null;
}

type PlayRequestInput = {
  source: PlaybackSource;
  track: DiscoveryResult;
  durationSeconds: number | null | undefined;
  searchId: string | null | undefined;
};

function trackPlayIdentity(track: DiscoveryResult) {
  return { title: track.title, artist: track.subtitle ?? '', artworkUrl: track.image_url };
}

function playRequest({ source, track, durationSeconds, searchId }: PlayRequestInput) {
  return {
    source,
    ...trackPlayIdentity(track),
    durationSeconds: durationSeconds ?? undefined,
    searchId: searchId ?? undefined,
    resultSignature: track.result_signature ?? undefined,
  };
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
  save: Pick<TrackSave, 'state' | 'failure'>;
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
  result: track,
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
  const onFeaturedPress = useOpenFeaturing(detailRoute);
  const save = useSaveTrack();
  const searchId = useDetailHandoff()?.searchId;
  const [playlistSheetVisible, setPlaylistSheetVisible] = useState(false);
  const wrongAlbum = useReportWrongAlbum(track);
  const playback = usePlayback();
  const te = trackExtras(track.extras);
  const owned = useOwnedTrack(te, { title: track.title, artist: track.subtitle });
  const trackSave = useTrackSave(track, owned);

  const albumName = te.album;
  const featured = resolveFeatured(track.extras, deezerFeatured, track.title, track.subtitle);

  const source = resolvePlaySource(te, owned);
  const playing = isResultPlaying(playback, te, owned);
  const isPreview = source?.kind === 'preview';

  const year = releasedYear(mbYear, te.year);

  const onTogglePlay = (): void => {
    if (playing) {
      playback.pause();
      return;
    }
    if (source === null) {
      return;
    }
    const durationSeconds = te.durationSeconds;
    void playback.play(playRequest({ source, track, durationSeconds, searchId }));
  };

  const resolveTrackIds = useCallback(async (): Promise<TrackId[]> => {
    if (owned !== null && !isOptimisticTrackId(owned.trackId)) {
      return [owned.trackId];
    }
    const saved = await save.mutateAsync(toCreateTrackRequest(track));
    return [saved.id];
  }, [owned, track, save]);

  const onAlbumPress = (): void => {
    if (track.subtitle !== null && albumName !== null) {
      void lateralNav.navigateTo(`${albumName} ${track.subtitle}`, 'album');
    }
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
    save: { state: trackSave.state, failure: trackSave.failure },
    onSave: trackSave.onSave,
    resolveTrackIds,
    playlistSheetVisible,
    setPlaylistSheetVisible,
    wrongAlbumReported: wrongAlbum.reported,
    onReportWrongAlbum: wrongAlbum.report,
    onAlbumPress,
    onFeaturedPress,
  };
}

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
import { useOwnedTrack } from './useOwnedTrack';
import { useReportWrongAlbum } from './useReportWrongAlbum';
import { useSaveTrack } from './useSaveTrack';
import { useTrackSave, type TrackSave } from './useTrackSave';
import { featuringRouteFor, type DetailRoute } from '../navigation';
import { isResultPlaying, resolvePlaySource } from '../play-source';
import { toCreateTrackRequest } from '../save-cache';

export type { LateralNavHandle };

function releasedYear(mbYear: number | undefined, extrasYear: number | null): string | null {
  if (mbYear != null && mbYear > 0) return String(mbYear);
  return extrasYear != null ? String(extrasYear) : null;
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
  const searchId = useDetailHandoff()?.searchId;
  const [playlistSheetVisible, setPlaylistSheetVisible] = useState(false);
  const wrongAlbum = useReportWrongAlbum(result);
  const playback = usePlayback();
  const te = trackExtras(result.extras);
  const owned = useOwnedTrack(te, { title: result.title, artist: result.subtitle });
  const trackSave = useTrackSave(result, owned);

  const albumName = te.album;
  const featured = resolveFeatured(result.extras, deezerFeatured, result.title, result.subtitle);

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

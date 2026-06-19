import { useCallback } from 'react';
import TrackPlayer from 'react-native-track-player';

import { audioRequestHeaders, audioStreamUrl } from '@features/playback/api/audio';

import { useQueueStore } from './queueStore';
import type { PlaybackTrack, QueueSource } from './types';

interface QueuePlaybackControls {
  playFromList: (tracks: readonly PlaybackTrack[], startIndex: number, source: QueueSource | null) => void;
  playTrack: (track: PlaybackTrack) => void;
  skipToNext: () => void;
  skipToPrevious: () => void;
}

async function loadNativeQueue(tracks: readonly PlaybackTrack[], startIndex: number): Promise<void> {
  const headers = await audioRequestHeaders();
  await TrackPlayer.reset();

  const rntpTracks = tracks.map((t) => {
    if (t.source.kind === 'preview') {
      return {
        url: t.source.previewUrl,
        title: t.title,
        artist: t.artist,
        artwork: t.artworkUrl ?? '',
      };
    }
    return {
      url: audioStreamUrl(t.source.trackId),
      title: t.title,
      artist: t.artist,
      artwork: t.artworkUrl ?? '',
      headers,
    };
  });

  await TrackPlayer.add(rntpTracks);
  await TrackPlayer.skip(startIndex);
  await TrackPlayer.play();
}

export function useQueuePlayback(): QueuePlaybackControls {
  const loadQueue = useQueueStore((s) => s.loadQueue);

  const playFromList = useCallback(
    (tracks: readonly PlaybackTrack[], startIndex: number, source: QueueSource | null) => {
      loadQueue(tracks, startIndex, source);
      const track = tracks[startIndex];
      if (track) {
        void loadNativeQueue(tracks, startIndex);
      }
    },
    [loadQueue],
  );

  const playTrack = useCallback(
    (track: PlaybackTrack) => {
      loadQueue([track], 0, null);
      void loadNativeQueue([track], 0);
    },
    [loadQueue],
  );

  const skipToNext = useCallback(() => {
    const nextTrack = useQueueStore.getState().skipToNext();
    if (nextTrack) {
      void TrackPlayer.skipToNext();
    }
  }, []);

  const skipToPrevious = useCallback(() => {
    const prevTrack = useQueueStore.getState().skipToPrevious();
    if (prevTrack) {
      void TrackPlayer.skipToPrevious();
    }
  }, []);

  return { playFromList, playTrack, skipToNext, skipToPrevious };
}

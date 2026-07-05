import TrackPlayer, { Event } from 'react-native-track-player';

import { RESTART_THRESHOLD_MS } from '@shared/playback/constants';
import { useQueueStore } from '@shared/playback/queueStore';

import { prefetchNext } from './audioPrefetch';

const RESTART_THRESHOLD_SECONDS = RESTART_THRESHOLD_MS / 1000;

// AIDEV-NOTE: Background playback service. With the native queue holding the
// full play order, transitions, auto-advance, and repeat are all native — there
// is no JS cold-load between tracks. This service only forwards lock-screen
// controls to the native player and mirrors the active-track index back into
// the store (the UI read-model) so screens follow background/lock-screen skips.
export async function playbackService() {
  TrackPlayer.addEventListener(Event.RemotePause, () => {
    void TrackPlayer.pause();
  });
  TrackPlayer.addEventListener(Event.RemotePlay, () => {
    void TrackPlayer.play();
  });
  TrackPlayer.addEventListener(Event.RemoteNext, () => {
    void TrackPlayer.skipToNext();
  });
  // Match the in-app previous button (FullPlayer.handlePrevious): past the
  // threshold, restart the current track; only step back a track when already
  // near the start. Read the position from the native player — the JS-side
  // position is frozen while the app is backgrounded/locked.
  TrackPlayer.addEventListener(Event.RemotePrevious, async () => {
    const { position } = await TrackPlayer.getProgress();
    if (position > RESTART_THRESHOLD_SECONDS) {
      await TrackPlayer.seekTo(0);
    } else {
      await TrackPlayer.skipToPrevious();
    }
  });
  TrackPlayer.addEventListener(Event.RemoteSeek, (data) => {
    void TrackPlayer.seekTo(data.position);
  });

  // The native player drives transitions (manual skip, gapless auto-advance,
  // repeat). Reflect its active position into the store so currentTrack() and
  // the queue UI stay in lockstep. syncCurrentIndex no-ops when already aligned.
  TrackPlayer.addEventListener(Event.PlaybackActiveTrackChanged, (data) => {
    if (typeof data.index === 'number') {
      useQueueStore.getState().syncCurrentIndex(data.index);
      // Download-ahead the next track (and swap its queue entry to the local
      // file) so the following auto-advance plays from disk with no buffering.
      void prefetchNext(data.index);
    }
  });
}

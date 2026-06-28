import TrackPlayer, { Event } from 'react-native-track-player';

import { useQueueStore } from '@shared/playback/queueStore';

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
  TrackPlayer.addEventListener(Event.RemotePrevious, () => {
    void TrackPlayer.skipToPrevious();
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
    }
  });
}

import { shouldRestartOnPrevious } from '@shared/playback/constants';
import { useQueueStore } from '@shared/playback/queueStore';
import { usePlayback } from '@shared/playback/usePlayback';
import type { PlaybackContextValue } from '@shared/playback/types';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';

function useQueueTransportState() {
  const { skipToNext, skipToPrevious, toggleShuffle, cycleRepeatMode } = useQueuePlayback();
  const shuffled = useQueueStore((s) => s.shuffled);
  const repeatMode = useQueueStore((s) => s.repeatMode);
  const hasNext = useQueueStore((s) => s.hasNext());
  const hasPrevious = useQueueStore((s) => s.hasPrevious());
  return { skipToNext, skipToPrevious, toggleShuffle, cycleRepeatMode, shuffled, repeatMode, hasNext, hasPrevious };
}

function statusFlagsOf(playback: PlaybackContextValue) {
  return {
    isPlaying: playback.status === 'playing',
    isEnded: playback.status === 'ended',
    isError: playback.status === 'error',
  };
}

function makeOnPrevious(playback: PlaybackContextValue, skipToPrevious: () => void) {
  return () => {
    if (shouldRestartOnPrevious(playback.positionMs)) {
      playback.seekTo(0);
    } else {
      skipToPrevious();
    }
  };
}

function restart(playback: PlaybackContextValue) {
  playback.seekTo(0);
  playback.resume();
}

function makeOnPlayPause(playback: PlaybackContextValue, isPlaying: boolean, isEnded: boolean) {
  if (isEnded) return () => restart(playback);
  if (isPlaying) return playback.pause;
  return playback.resume;
}

export function usePlaybackTransport() {
  const playback = usePlayback();
  const queue = useQueueTransportState();
  const { isPlaying, isEnded, isError } = statusFlagsOf(playback);
  const onPrevious = makeOnPrevious(playback, queue.skipToPrevious);
  const onPlayPause = makeOnPlayPause(playback, isPlaying, isEnded);

  return { ...playback, ...queue, isPlaying, isEnded, isError, onPrevious, onPlayPause };
}

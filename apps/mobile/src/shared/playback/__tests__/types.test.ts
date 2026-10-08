import { expectType, type IsAssignable, type Not } from '@testing/typeAssertions';

import type { PlaybackContextValue as NewPlaybackContextValue } from '@shared/playback/playbackContextValue';
import type { PlaybackTrack as NewPlaybackTrack } from '@shared/playback/playbackTrack';
import type {
  QueueSource as NewQueueSource,
  RepeatMode as NewRepeatMode,
} from '@shared/playback/queueSource';
import type {
  PlaybackContextValue as ShimPlaybackContextValue,
  PlaybackTrack as ShimPlaybackTrack,
  QueueSource as ShimQueueSource,
  RepeatMode as ShimRepeatMode,
} from '@shared/playback/types';

describe('playback types shim', () => {
  it('keeps PlaybackTrack assignable both ways', () => {
    expectType<IsAssignable<ShimPlaybackTrack, NewPlaybackTrack>>();
    expectType<IsAssignable<NewPlaybackTrack, ShimPlaybackTrack>>();
  });

  it('keeps PlaybackContextValue assignable both ways', () => {
    expectType<IsAssignable<ShimPlaybackContextValue, NewPlaybackContextValue>>();
    expectType<IsAssignable<NewPlaybackContextValue, ShimPlaybackContextValue>>();
  });

  it('keeps QueueSource assignable both ways', () => {
    expectType<IsAssignable<ShimQueueSource, NewQueueSource>>();
    expectType<IsAssignable<NewQueueSource, ShimQueueSource>>();
  });

  it('keeps RepeatMode assignable both ways and refuses shuffle', () => {
    expectType<IsAssignable<ShimRepeatMode, NewRepeatMode>>();
    expectType<IsAssignable<NewRepeatMode, ShimRepeatMode>>();
    expectType<Not<IsAssignable<'shuffle', ShimRepeatMode>>>();
  });
});

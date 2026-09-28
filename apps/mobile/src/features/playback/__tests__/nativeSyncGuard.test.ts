import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { loadNativeQueue } from '../loadNativeTrack';
import { beginNativeLoad, endNativeLoad, shouldApplyActiveIndex } from '../nativeSyncGuard';

import { previewTrack } from './fixtures';

beforeEach(() => {
  endNativeLoad();
});

describe('shouldApplyActiveIndex — with no native load in flight', () => {
  it('applies whatever index the native player reports', () => {
    expect(shouldApplyActiveIndex(0)).toBe(true);
    expect(shouldApplyActiveIndex(3)).toBe(true);
  });
});

describe('shouldApplyActiveIndex — while a load targeting a non-zero index is in flight', () => {
  it('ignores the priming index-0 event the native add fires first', () => {
    beginNativeLoad(4);

    expect(shouldApplyActiveIndex(0)).toBe(false);
  });

  it('keeps ignoring repeated priming index-0 events until the real one arrives', () => {
    beginNativeLoad(4);

    expect(shouldApplyActiveIndex(0)).toBe(false);
    expect(shouldApplyActiveIndex(0)).toBe(false);
  });

  it('applies the real target index and then clears the guard', () => {
    beginNativeLoad(4);

    expect(shouldApplyActiveIndex(4)).toBe(true);
    expect(shouldApplyActiveIndex(0)).toBe(true);
  });
});

describe('shouldApplyActiveIndex — while a load targeting index 0 is in flight', () => {
  it('applies the index-0 event because priming to the first track is the real target', () => {
    beginNativeLoad(0);

    expect(shouldApplyActiveIndex(0)).toBe(true);
  });
});

describe('endNativeLoad — cancelling an in-flight load', () => {
  it('releases the guard so a subsequent index-0 event applies', () => {
    beginNativeLoad(4);
    endNativeLoad();

    expect(shouldApplyActiveIndex(0)).toBe(true);
  });
});

describe('the sync guard under rapid skips and overlapping loads', () => {
  function makeTracks(count: number): PlaybackTrack[] {
    return Array.from({ length: count }, (_, i) =>
      previewTrack({
        source: { kind: 'preview', previewUrl: `https://cdn.example/${i}.mp3` },
        title: `Track ${i}`,
      }),
    );
  }

  function fireActiveTrackChanged(index: number, key: string | undefined): void {
    if (!shouldApplyActiveIndex(index)) return;
    useQueueStore.getState().syncCurrentIndex(index, key);
  }

  function nativePlayer(tracks: readonly PlaybackTrack[]) {
    const ordered = orderedQueueTracks({
      tracks,
      playOrder: tracks.map((_, i) => i),
    });
    let index = 0;
    return {
      get index() {
        return index;
      },
      seekTo(next: number) {
        index = next;
        fireActiveTrackChanged(index, trackKey(ordered[index]!));
      },
      skipNext() {
        if (index < ordered.length - 1) this.seekTo(index + 1);
      },
      skipPrevious() {
        if (index > 0) this.seekTo(index - 1);
      },
    };
  }

  beforeEach(() => {
    endNativeLoad();
    useQueueStore.getState().clearQueue();
  });

  describe('rapid skip burst — store cursor stays glued to the native active track', () => {
    it('tracks every native skip in a fast burst and ends in sync', async () => {
      const tracks = makeTracks(10);
      useQueueStore.getState().loadQueue(tracks, 0, null);
      await loadNativeQueue(tracks, 0, { autoplay: false });

      const native = nativePlayer(tracks);
      for (let i = 0; i < 6; i++) native.skipNext();

      expect(native.index).toBe(6);
      expect(useQueueStore.getState().currentIndex).toBe(6);
      expect(useQueueStore.getState().currentTrack()).toBe(tracks[6]);
    });
  });

  describe('a load whose target event is lost to a skip burst — no wedge', () => {
    it('still applies a later skip-to-top index-0 event instead of suppressing it', async () => {
      const tracks = makeTracks(10);
      useQueueStore.getState().loadQueue(tracks, 5, null);

      await loadNativeQueue(tracks, 5, { autoplay: false });

      fireActiveTrackChanged(0, trackKey(tracks[0]!));

      expect(useQueueStore.getState().currentIndex).toBe(0);
      expect(useQueueStore.getState().currentTrack()).toBe(tracks[0]);
    });
  });

  describe('overlapping loads — each keeps its own suppression', () => {
    it('suppresses priming index-0 until every in-flight load has released', () => {
      useQueueStore.getState().loadQueue(makeTracks(10), 0, null);

      const first = beginNativeLoad(5);
      const second = beginNativeLoad(8);

      expect(shouldApplyActiveIndex(0)).toBe(false);

      endNativeLoad(first);
      expect(shouldApplyActiveIndex(0)).toBe(false);

      endNativeLoad(second);
      expect(shouldApplyActiveIndex(0)).toBe(true);
    });

    it('reconciles the store to the native resting track after overlapping loads settle', () => {
      const tracks = makeTracks(10);
      useQueueStore.getState().loadQueue(tracks, 0, null);

      const first = beginNativeLoad(5);
      const second = beginNativeLoad(8);
      endNativeLoad(first);
      endNativeLoad(second);

      const native = nativePlayer(tracks);
      native.seekTo(8);
      for (let i = 0; i < 3; i++) native.skipPrevious();

      expect(native.index).toBe(5);
      expect(useQueueStore.getState().currentIndex).toBe(5);
    });
  });
});

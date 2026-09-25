import React from 'react';
import { act, renderHook } from '@testing-library/react-native';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { ApiError } from '@shared/api-client';
import { asTrackId } from '@shared/api-client/ids';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';
import { usePlayback } from '@shared/playback/usePlayback';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';

import { libraryTrack, previewTrack } from '../../__tests__/fixtures';
import { WebPlaybackProvider } from '../webPlaybackProvider';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const presign = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

const HAVE_ENOUGH_DATA = 4;

class FakeAudio {
  src = '';
  currentTime = 0;
  duration = Number.NaN;
  playbackRate = 1;
  paused = true;
  ended = false;
  readyState = 0;
  error: { code: number; message: string } | null = null;
  playRejection: Error | null = null;
  private readonly listeners = new Map<string, Set<() => void>>();

  addEventListener(type: string, listener: () => void): void {
    const forType = this.listeners.get(type) ?? new Set();
    forType.add(listener);
    this.listeners.set(type, forType);
  }

  removeEventListener(type: string, listener: () => void): void {
    this.listeners.get(type)?.delete(listener);
  }

  listenerCount(): number {
    return [...this.listeners.values()].reduce((sum, forType) => sum + forType.size, 0);
  }

  emit(type: string): void {
    for (const listener of this.listeners.get(type) ?? []) listener();
  }

  play(): Promise<void> {
    if (this.playRejection) return Promise.reject(this.playRejection);
    this.paused = false;
    this.ended = false;
    this.emit('play');
    return Promise.resolve();
  }

  pause(): void {
    if (this.paused) return;
    this.paused = true;
    this.emit('pause');
  }

  removeAttribute(name: string): void {
    if (name === 'src') this.src = '';
  }

  load(): void {
    this.readyState = 0;
    this.error = null;
  }

  bufferEnough(): void {
    this.readyState = HAVE_ENOUGH_DATA;
    this.emit('playing');
  }

  reachEnd(): void {
    this.paused = true;
    this.ended = true;
    this.emit('pause');
    this.emit('ended');
  }

  failWith(code: number, message = ''): void {
    this.error = { code, message };
    this.emit('error');
  }
}

function presignedUrl(trackId: string, attempt = 1): ResolvedAudioUrl {
  return { trackId, url: `https://bucket.example/${trackId}.mp3?sig=${attempt}`, version: 'v1' };
}

function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve: (value: T) => void = () => undefined;
  const promise = new Promise<T>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

function trackNamed(id: string): PlaybackTrack {
  return libraryTrack({ source: { kind: 'library', trackId: asTrackId(id) }, title: id });
}

function renderWebPlayback() {
  const audio = new FakeAudio();
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <WebPlaybackProvider createAudio={() => audio as unknown as HTMLAudioElement}>
      {children}
    </WebPlaybackProvider>
  );
  const rendered = renderHook(() => usePlayback(), { wrapper });
  return { audio, rendered, playback: () => rendered.result.current };
}

async function playing(track: PlaybackTrack = libraryTrack()) {
  const view = renderWebPlayback();
  await act(() => view.playback().play(track));
  act(() => view.audio.bufferEnough());
  return view;
}

beforeEach(() => {
  presign.mockReset();
  presign.mockImplementation(async (ids) => ids.map((id) => presignedUrl(id)));
  useQueueStore.getState().clearQueue();
});

describe('WebPlaybackProvider', () => {
  it('plays a library track from the presigned url of POST /v1/audio-urls', async () => {
    const { audio, playback } = await playing();

    expect(presign).toHaveBeenCalledWith(['trk-1']);
    expect(audio.src).toBe(presignedUrl('trk-1').url);
    expect(playback()).toMatchObject({ status: 'playing', track: { title: 'A Title' } });
  });

  it('reports loading while the presigned url is being fetched, then loading until audio buffers', async () => {
    const pending = deferred<ResolvedAudioUrl[]>();
    presign.mockReturnValueOnce(pending.promise);
    const { audio, playback } = renderWebPlayback();

    act(() => void playback().play(libraryTrack()));
    const whilePresigning = playback().status;
    await act(async () => pending.resolve([presignedUrl('trk-1')]));

    expect(whilePresigning).toBe('loading');
    expect(audio.paused).toBe(false);
    expect(playback().status).toBe('loading');
  });

  it('plays a preview track from its preview url without presigning', async () => {
    const { audio, playback } = await playing(previewTrack());

    expect(presign).not.toHaveBeenCalled();
    expect(audio.src).toBe('https://cdn.example/p.mp3');
    expect(playback().status).toBe('playing');
  });

  it('pauses and resumes the loaded track', async () => {
    const { audio, playback } = await playing();

    act(() => playback().pause());
    const afterPause = playback().status;
    act(() => playback().resume());

    expect(afterPause).toBe('paused');
    expect(audio.paused).toBe(false);
    expect(playback().status).toBe('playing');
  });

  it('ignores resume when nothing is loaded', () => {
    const { audio, playback } = renderWebPlayback();

    act(() => playback().resume());

    expect(audio.paused).toBe(true);
    expect(playback().status).toBe('idle');
  });

  it('seeks the audio element and reports the position and duration it plays at', async () => {
    const { audio, playback } = await playing();

    act(() => playback().seekTo(42_500));
    audio.duration = 180.25;
    act(() => {
      audio.emit('timeupdate');
      audio.emit('durationchange');
    });

    expect(audio.currentTime).toBe(42.5);
    expect(playback()).toMatchObject({ positionMs: 42_500, durationMs: 180_250 });
  });

  it('reports a zero duration while the stream length is unknown', async () => {
    const { audio, playback } = await playing();

    audio.duration = Number.POSITIVE_INFINITY;
    act(() => audio.emit('durationchange'));

    expect(playback().durationMs).toBe(0);
  });

  it('sets the playback rate on the audio element', async () => {
    const { audio, playback } = await playing();

    act(() => playback().setRate(1.5));

    expect(audio.playbackRate).toBe(1.5);
  });

  it('reports ended when the track plays to its end', async () => {
    const { audio, playback } = await playing();

    act(() => audio.reachEnd());

    expect(playback().status).toBe('ended');
  });

  it('reports paused when the browser refuses to start playback', async () => {
    const view = renderWebPlayback();
    view.audio.playRejection = Object.assign(new Error('autoplay denied'), {
      name: 'NotAllowedError',
    });

    await act(() => view.playback().play(libraryTrack()));

    expect(view.playback().status).toBe('paused');
  });

  it.each([
    [2, 'network'],
    [3, 'decode'],
    [4, 'unknown'],
  ] as const)('classifies media error code %i as %s', async (code, kind) => {
    const { audio, playback } = await playing();

    act(() => audio.failWith(code, 'MEDIA_ELEMENT_ERROR at https://bucket.example/x?sig=1'));

    expect(playback()).toMatchObject({ status: 'error', errorKind: kind });
    expect(playback().errorMessage).not.toContain('sig=1');
  });

  it('falls back to a readable message when the media error carries none', async () => {
    const { audio, playback } = await playing();

    act(() => audio.failWith(2));

    expect(playback().errorMessage).toBe('The audio could not be played');
  });

  it('retries a failed track with a fresh presigned url and plays it', async () => {
    const { audio, playback } = await playing();
    act(() => audio.failWith(2));
    presign.mockResolvedValueOnce([presignedUrl('trk-1', 2)]);

    await act(async () => playback().retry());
    act(() => audio.bufferEnough());

    expect(presign).toHaveBeenCalledTimes(2);
    expect(audio.src).toBe(presignedUrl('trk-1', 2).url);
    expect(playback()).toMatchObject({ status: 'playing', errorKind: null });
  });

  it('does nothing on retry when no track was played', () => {
    const { playback } = renderWebPlayback();

    act(() => playback().retry());

    expect(presign).not.toHaveBeenCalled();
    expect(playback().status).toBe('idle');
  });

  it('surfaces a failed presign as an error classified from its status', async () => {
    presign.mockRejectedValueOnce(new ApiError(503, 'unavailable'));
    const { audio, playback } = renderWebPlayback();

    await act(() => playback().play(libraryTrack()));

    expect(audio.src).toBe('');
    expect(playback()).toMatchObject({ status: 'error', errorKind: 'network' });
  });

  it('surfaces a presign response without the track as not found', async () => {
    presign.mockResolvedValueOnce([presignedUrl('another-track')]);
    const { audio, playback } = renderWebPlayback();

    await act(() => playback().play(libraryTrack()));

    expect(audio.src).toBe('');
    expect(playback()).toMatchObject({ status: 'error', errorKind: 'not_found' });
  });

  it('ignores a media error from the previous source while the next one is presigning', async () => {
    const { audio, playback } = await playing();
    presign.mockReturnValueOnce(deferred<ResolvedAudioUrl[]>().promise);

    act(() => void playback().play(trackNamed('trk-2')));
    act(() => audio.failWith(2));

    expect(playback()).toMatchObject({ status: 'loading', errorKind: null });
  });

  it('keeps the latest track when an earlier presign resolves after it', async () => {
    const first = deferred<ResolvedAudioUrl[]>();
    presign.mockReturnValueOnce(first.promise);
    const { audio, playback } = renderWebPlayback();

    act(() => void playback().play(trackNamed('trk-1')));
    await act(() => playback().play(trackNamed('trk-2')));
    await act(async () => first.resolve([presignedUrl('trk-1')]));

    expect(audio.src).toBe(presignedUrl('trk-2').url);
    expect(playback().track?.title).toBe('trk-2');
  });

  it('stops playback, releases the source and returns to idle', async () => {
    const { audio, playback } = await playing();

    act(() => playback().stop());

    expect(audio.paused).toBe(true);
    expect(audio.src).toBe('');
    expect(playback()).toMatchObject({ status: 'idle', track: null });
  });

  it('stops playback, releases the source and clears the queue on sign-out', async () => {
    const { audio, playback } = await playing();
    useQueueStore.getState().loadQueue([trackNamed('trk-1'), trackNamed('trk-2')], 0, null);

    act(() => runSignOutCleanups());

    expect(audio.src).toBe('');
    expect(audio.paused).toBe(true);
    expect(playback()).toMatchObject({ status: 'idle', track: null });
    expect(useQueueStore.getState().tracks).toHaveLength(0);
  });

  it('never plays a presign that resolves after sign-out', async () => {
    const pending = deferred<ResolvedAudioUrl[]>();
    presign.mockReturnValueOnce(pending.promise);
    const { audio, playback } = renderWebPlayback();

    act(() => void playback().play(libraryTrack()));
    act(() => runSignOutCleanups());
    await act(async () => pending.resolve([presignedUrl('trk-1')]));

    expect(audio.src).toBe('');
    expect(playback()).toMatchObject({ status: 'idle', track: null });
  });

  it('releases the source and its listeners on unmount', async () => {
    const { audio, rendered } = await playing();

    rendered.unmount();

    expect(audio.src).toBe('');
    expect(audio.listenerCount()).toBe(0);
  });

  it('plays a single track outside any queue', async () => {
    useQueueStore.getState().loadQueue([trackNamed('trk-1'), trackNamed('trk-2')], 0, null);
    const { playback } = renderWebPlayback();

    await act(() => playback().play(trackNamed('trk-3')));

    expect(useQueueStore.getState().tracks).toHaveLength(0);
  });

  it('starts a queue at its start track', async () => {
    const queue = [trackNamed('trk-1'), trackNamed('trk-2')];
    const { audio, playback } = renderWebPlayback();

    await act(() => playback().startQueue(queue, 1));

    expect(presign).toHaveBeenCalledWith(['trk-2']);
    expect(audio.src).toBe(presignedUrl('trk-2').url);
    expect(playback().track?.title).toBe('trk-2');
  });

  it('starts a queue paused at the resume position when autoplay is off', async () => {
    const { audio, playback } = renderWebPlayback();

    await act(() =>
      playback().startQueue([trackNamed('trk-1')], 0, { autoplay: false, startPositionMs: 30_000 }),
    );

    expect(audio.currentTime).toBe(30);
    expect(audio.paused).toBe(true);
    expect(playback().status).toBe('paused');
  });

  it('ignores a start index outside the queue', async () => {
    const { playback } = renderWebPlayback();

    await act(() => playback().startQueue([trackNamed('trk-1')], 5));

    expect(presign).not.toHaveBeenCalled();
    expect(playback().status).toBe('idle');
  });

  it('skips forward and back through the queue store', async () => {
    const queue = [trackNamed('trk-1'), trackNamed('trk-2')];
    useQueueStore.getState().loadQueue(queue, 0, null);
    const { audio, playback } = renderWebPlayback();

    await act(() => playback().skipNext());
    const afterNext = audio.src;
    await act(() => playback().skipPrevious());

    expect(afterNext).toBe(presignedUrl('trk-2').url);
    expect(audio.src).toBe(presignedUrl('trk-1').url);
  });

  it('plays the queue track the store moved to', async () => {
    useQueueStore.getState().loadQueue([trackNamed('trk-1'), trackNamed('trk-2')], 0, null);
    useQueueStore.getState().skipToIndex(1);
    const { audio, playback } = renderWebPlayback();

    await act(() => playback().skipToQueueIndex(1));

    expect(audio.src).toBe(presignedUrl('trk-2').url);
  });

  it('leaves the playing track alone on queue edits the store already holds', async () => {
    const { audio, playback } = await playing();
    const src = audio.src;

    await act(async () => {
      await playback().appendToQueue(trackNamed('trk-2'));
      await playback().insertNext(trackNamed('trk-3'), 1);
      await playback().reorderUpcoming([]);
      await playback().removeQueueIndex(1);
    });

    expect(audio.src).toBe(src);
    expect(playback().status).toBe('playing');
  });
});

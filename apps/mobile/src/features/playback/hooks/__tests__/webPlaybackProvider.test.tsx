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
  currentSrc = '';
  private srcAttribute = '';
  currentTime = 0;
  duration = Number.NaN;
  playbackRate = 1;
  paused = true;
  ended = false;
  readyState = 0;
  error: { code: number; message: string } | null = null;
  playRejection: Error | null = null;
  private readonly listeners = new Map<string, Set<() => void>>();

  get src(): string {
    return this.srcAttribute;
  }

  set src(url: string) {
    this.srcAttribute = url;
    this.load();
  }

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
    if (name === 'src') this.srcAttribute = '';
  }

  load(): void {
    this.currentSrc = this.srcAttribute;
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

  failWithoutDetail(): void {
    this.error = null;
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

function renderWebPlaybackWithClock(now: () => number) {
  const audio = new FakeAudio();
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <WebPlaybackProvider createAudio={() => audio as unknown as HTMLAudioElement} now={now}>
      {children}
    </WebPlaybackProvider>
  );
  const rendered = renderHook(() => usePlayback(), { wrapper });
  return { audio, playback: () => rendered.result.current };
}

const PRESIGN_TTL_MS = 60 * 60 * 1000;

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

  it('reports loading while playback stalls for data', async () => {
    const { audio, playback } = await playing();

    audio.readyState = 2;
    act(() => audio.emit('waiting'));

    expect(playback().status).toBe('loading');
  });

  it('reports playing once enough data is buffered to play ahead', async () => {
    const { audio, playback } = renderWebPlayback();
    await act(() => playback().play(libraryTrack()));

    audio.readyState = 3;
    act(() => audio.emit('playing'));

    expect(playback().status).toBe('playing');
  });

  it('reports ended when only the ended event arrives', async () => {
    const { audio, playback } = await playing();

    audio.paused = true;
    audio.ended = true;
    act(() => audio.emit('ended'));

    expect(playback().status).toBe('ended');
  });

  it('reports paused after seeking back into a track that had ended', async () => {
    const { audio, playback } = await playing();
    act(() => audio.reachEnd());

    audio.ended = false;
    act(() => audio.emit('seeked'));

    expect(playback().status).toBe('paused');
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
    expect(playback().errorMessage).toContain('MEDIA_ELEMENT_ERROR');
    expect(playback().errorMessage).not.toContain('sig=1');
  });

  it('falls back to a readable message when the media error carries none', async () => {
    const { audio, playback } = await playing();

    act(() => audio.failWith(2));

    expect(playback().errorMessage).toBe('The audio could not be played');
  });

  it('reports an unknown error when the element fails without a media error', async () => {
    const { audio, playback } = await playing();

    act(() => audio.failWithoutDetail());

    expect(playback()).toMatchObject({
      status: 'error',
      errorKind: 'unknown',
      errorMessage: 'The audio could not be played',
    });
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
    expect(playback()).toMatchObject({
      status: 'error',
      errorKind: 'not_found',
      errorMessage: 'No audio is available for this track',
    });
  });

  it('ignores a media error from the previous source while the next one is presigning', async () => {
    const { audio, playback } = await playing();
    presign.mockReturnValueOnce(deferred<ResolvedAudioUrl[]>().promise);

    act(() => void playback().play(trackNamed('trk-2')));
    act(() => audio.failWith(2));

    expect(playback()).toMatchObject({ status: 'loading', errorKind: null });
  });

  it('stays loading when the released source reports playback events during the presign', async () => {
    const { audio, playback } = await playing();
    presign.mockReturnValueOnce(deferred<ResolvedAudioUrl[]>().promise);

    act(() => void playback().play(trackNamed('trk-2')));
    act(() => {
      audio.emit('pause');
      audio.emit('waiting');
    });

    expect(playback()).toMatchObject({ status: 'loading', track: { title: 'trk-2' } });
  });

  it('silences the previous track as soon as the next one starts presigning', async () => {
    const { audio, playback } = await playing();
    presign.mockReturnValueOnce(deferred<ResolvedAudioUrl[]>().promise);

    act(() => void playback().play(trackNamed('trk-2')));

    expect(audio.paused).toBe(true);
    expect(audio.currentSrc).toBe('');
  });

  it('ignores a presign started before a stop even after another track starts loading', async () => {
    const beforeStop = deferred<ResolvedAudioUrl[]>();
    presign.mockReturnValueOnce(beforeStop.promise);
    presign.mockReturnValueOnce(deferred<ResolvedAudioUrl[]>().promise);
    const { audio, playback } = renderWebPlayback();

    act(() => void playback().play(trackNamed('trk-1')));
    act(() => playback().stop());
    act(() => void playback().play(trackNamed('trk-2')));
    await act(async () => beforeStop.resolve([presignedUrl('trk-1')]));

    expect(audio.src).toBe('');
    expect(playback()).toMatchObject({ status: 'loading', track: { title: 'trk-2' } });
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
    expect(audio.currentSrc).toBe('');
    expect(playback()).toMatchObject({ status: 'idle', track: null });
  });

  it('stops playback, releases the source and clears the queue on sign-out', async () => {
    const { audio, playback } = await playing();
    useQueueStore.getState().loadQueue([trackNamed('trk-1'), trackNamed('trk-2')], 0, null);

    act(() => runSignOutCleanups());

    expect(audio.currentSrc).toBe('');
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

    expect(audio.currentSrc).toBe('');
    expect(audio.listenerCount()).toBe(0);
  });

  it('leaves the audio element alone on a sign-out after unmount', async () => {
    const { audio, rendered } = await playing();
    rendered.unmount();
    audio.src = 'https://elsewhere.example/audio.mp3';

    act(() => runSignOutCleanups());

    expect(audio.currentSrc).toBe('https://elsewhere.example/audio.mp3');
  });

  it('creates its own audio element when none is injected', async () => {
    const created: FakeAudio[] = [];
    const globalAudio = globalThis as unknown as { Audio?: new () => FakeAudio };
    globalAudio.Audio = class extends FakeAudio {
      constructor() {
        super();
        created.push(this);
      }
    };
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <WebPlaybackProvider>{children}</WebPlaybackProvider>
    );
    const { result } = renderHook(() => usePlayback(), { wrapper });

    await act(() => result.current.play(libraryTrack()));
    delete globalAudio.Audio;

    expect(created).toHaveLength(1);
    expect(created[0]?.src).toBe(presignedUrl('trk-1').url);
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
      await expect(playback().appendToQueue(trackNamed('trk-2'))).resolves.toBeUndefined();
      await expect(playback().insertNext(trackNamed('trk-3'), 1)).resolves.toBeUndefined();
      await expect(playback().reorderUpcoming([])).resolves.toBeUndefined();
      await expect(playback().removeQueueIndex(1)).resolves.toBeUndefined();
    });

    expect(audio.src).toBe(src);
    expect(playback().status).toBe('playing');
  });

  it('restarts the current track instead of stepping back past the restart threshold', async () => {
    const { audio, playback } = await playing(trackNamed('trk-2'));
    audio.currentTime = 5;

    await act(() => playback().skipPrevious());

    expect(audio.src).toBe(presignedUrl('trk-2').url);
    expect(audio.currentTime).toBe(0);
  });

  it('steps to the previous queue track within the restart threshold', async () => {
    const queue = [trackNamed('trk-1'), trackNamed('trk-2')];
    useQueueStore.getState().loadQueue(queue, 1, null);
    const { audio, playback } = renderWebPlayback();
    await act(() => playback().startQueue(queue, 1));
    audio.currentTime = 1;

    await act(() => playback().skipPrevious());

    expect(audio.src).toBe(presignedUrl('trk-1').url);
  });

  it('advances to the next queue track when the current one ends', async () => {
    const queue = [trackNamed('trk-1'), trackNamed('trk-2')];
    useQueueStore.getState().loadQueue(queue, 0, null);
    const { audio, playback } = renderWebPlayback();
    await act(() => playback().startQueue(queue, 0));

    await act(async () => audio.reachEnd());

    expect(audio.src).toBe(presignedUrl('trk-2').url);
    expect(playback().track?.title).toBe('trk-2');
  });

  it('stays ended with no further track when the last one ends and repeat is off', async () => {
    const { audio, playback } = await playing(trackNamed('trk-2'));

    act(() => audio.reachEnd());

    expect(playback().status).toBe('ended');
    expect(audio.src).toBe(presignedUrl('trk-2').url);
  });

  it('wraps to the first queue track when the last one ends under repeat all', async () => {
    const queue = [trackNamed('trk-1'), trackNamed('trk-2')];
    useQueueStore.getState().loadQueue(queue, 1, null);
    useQueueStore.getState().setRepeatMode('all');
    const { audio, playback } = renderWebPlayback();
    await act(() => playback().startQueue(queue, 1));

    await act(async () => audio.reachEnd());

    expect(audio.src).toBe(presignedUrl('trk-1').url);
    expect(playback().track?.title).toBe('trk-1');
  });

  it('restarts the current track when it ends under repeat one', async () => {
    const queue = [trackNamed('trk-1')];
    useQueueStore.getState().loadQueue(queue, 0, null);
    useQueueStore.getState().setRepeatMode('one');
    const { audio, playback } = renderWebPlayback();
    await act(() => playback().startQueue(queue, 0));
    act(() => audio.bufferEnough());
    audio.currentTime = 120;

    act(() => audio.reachEnd());

    expect(audio.currentTime).toBe(0);
    expect(playback().status).toBe('playing');
    expect(presign).toHaveBeenCalledTimes(1);
  });

  it('re-presigns and resumes at the same position when seeking a stale presigned url', async () => {
    let clock = 0;
    const now = () => clock;
    const { audio, playback } = renderWebPlaybackWithClock(now);
    await act(() => playback().play(trackNamed('trk-1')));
    act(() => audio.bufferEnough());

    presign.mockResolvedValueOnce([presignedUrl('trk-1', 2)]);
    clock += PRESIGN_TTL_MS;
    await act(async () => playback().seekTo(60_000));

    expect(presign).toHaveBeenCalledTimes(2);
    expect(audio.src).toBe(presignedUrl('trk-1', 2).url);
    expect(audio.currentTime).toBe(60);
  });

  it('recovers from a network media error once the presigned url is stale', async () => {
    let clock = 0;
    const now = () => clock;
    const { audio, playback } = renderWebPlaybackWithClock(now);
    await act(() => playback().play(trackNamed('trk-1')));
    act(() => audio.bufferEnough());
    audio.currentTime = 12;
    act(() => audio.pause());

    presign.mockResolvedValueOnce([presignedUrl('trk-1', 2)]);
    clock += PRESIGN_TTL_MS;
    await act(async () => audio.failWith(2));
    act(() => audio.bufferEnough());

    expect(presign).toHaveBeenCalledTimes(2);
    expect(audio.src).toBe(presignedUrl('trk-1', 2).url);
    expect(audio.currentTime).toBe(12);
    expect(playback()).toMatchObject({ status: 'playing', errorMessage: null });
  });

  it('ignores a network media error on a fresh presigned url', async () => {
    const { audio, playback } = await playing(trackNamed('trk-1'));

    act(() => audio.failWith(2));

    expect(presign).toHaveBeenCalledTimes(1);
    expect(playback()).toMatchObject({ status: 'error', errorKind: 'network' });
  });

  it('gives up with a retryable error when the re-presigned url errors again', async () => {
    let clock = 0;
    const now = () => clock;
    const { audio, playback } = renderWebPlaybackWithClock(now);
    await act(() => playback().play(trackNamed('trk-1')));
    act(() => audio.bufferEnough());

    presign.mockResolvedValueOnce([presignedUrl('trk-1', 2)]);
    clock += PRESIGN_TTL_MS;
    await act(async () => audio.failWith(2));
    act(() => audio.failWith(2));

    expect(presign).toHaveBeenCalledTimes(2);
    expect(playback()).toMatchObject({ status: 'error', errorKind: 'network' });
  });

  it('does not re-presign a seek while the initial presign is still pending', async () => {
    presign.mockReturnValueOnce(deferred<ResolvedAudioUrl[]>().promise);
    const { audio, playback } = renderWebPlayback();

    act(() => void playback().play(trackNamed('trk-1')));
    act(() => playback().seekTo(1_000));

    expect(presign).toHaveBeenCalledTimes(1);
    expect(audio.src).toBe('');
  });

  it('treats a url right at the ttl margin as fresh, and just past it as stale', async () => {
    let clock = 0;
    const now = () => clock;
    const { audio, playback } = renderWebPlaybackWithClock(now);
    await act(() => playback().play(trackNamed('trk-1')));
    act(() => audio.bufferEnough());

    clock += PRESIGN_TTL_MS - 60_000 - 1;
    act(() => playback().seekTo(10_000));

    expect(presign).toHaveBeenCalledTimes(1);

    clock += 1;
    presign.mockResolvedValueOnce([presignedUrl('trk-1', 2)]);
    await act(async () => playback().seekTo(20_000));

    expect(presign).toHaveBeenCalledTimes(2);
  });

  it('discards a stale-triggered re-presign that resolves after a newer track started', async () => {
    let clock = 0;
    const now = () => clock;
    const { audio, playback } = renderWebPlaybackWithClock(now);
    await act(() => playback().play(trackNamed('trk-1')));
    act(() => audio.bufferEnough());

    const stalePresign = deferred<ResolvedAudioUrl[]>();
    presign.mockReturnValueOnce(stalePresign.promise);
    clock += PRESIGN_TTL_MS;
    act(() => void playback().resume());
    await act(() => playback().play(trackNamed('trk-2')));
    await act(async () => stalePresign.resolve([presignedUrl('trk-1', 2)]));

    expect(audio.src).toBe(presignedUrl('trk-2').url);
    expect(playback().track?.title).toBe('trk-2');
  });

  it('reports loading while a stale-error recovery is in flight', async () => {
    let clock = 0;
    const now = () => clock;
    const { audio, playback } = renderWebPlaybackWithClock(now);
    await act(() => playback().play(trackNamed('trk-1')));
    act(() => audio.bufferEnough());

    const stalePresign = deferred<ResolvedAudioUrl[]>();
    presign.mockReturnValueOnce(stalePresign.promise);
    clock += PRESIGN_TTL_MS;
    act(() => void audio.failWith(2));

    expect(playback().status).toBe('loading');

    await act(async () => stalePresign.resolve([presignedUrl('trk-1', 2)]));
  });

  it('resumes recovery attempts for a newly loaded track after a previous track gave up', async () => {
    let clock = 0;
    const now = () => clock;
    const { audio, playback } = renderWebPlaybackWithClock(now);
    await act(() => playback().play(trackNamed('trk-1')));
    act(() => audio.bufferEnough());

    presign.mockResolvedValueOnce([presignedUrl('trk-1', 2)]);
    clock += PRESIGN_TTL_MS;
    await act(async () => audio.failWith(2));
    act(() => audio.failWith(2));
    expect(playback()).toMatchObject({ status: 'error', errorKind: 'network' });

    await act(() => playback().play(trackNamed('trk-2')));
    act(() => audio.bufferEnough());
    presign.mockResolvedValueOnce([presignedUrl('trk-2', 2)]);
    clock += PRESIGN_TTL_MS;
    await act(async () => audio.failWith(2));
    act(() => audio.bufferEnough());

    expect(audio.src).toBe(presignedUrl('trk-2', 2).url);
    expect(playback()).toMatchObject({ status: 'playing', errorMessage: null });
  });

  it('re-presigns before restarting a repeat-one track whose presigned url has gone stale', async () => {
    let clock = 0;
    const now = () => clock;
    const queue = [trackNamed('trk-1')];
    useQueueStore.getState().loadQueue(queue, 0, null);
    useQueueStore.getState().setRepeatMode('one');
    const { audio, playback } = renderWebPlaybackWithClock(now);
    await act(() => playback().startQueue(queue, 0));
    act(() => audio.bufferEnough());
    audio.currentTime = 90;

    presign.mockResolvedValueOnce([presignedUrl('trk-1', 2)]);
    clock += PRESIGN_TTL_MS;
    await act(async () => audio.reachEnd());
    act(() => audio.bufferEnough());

    expect(presign).toHaveBeenCalledTimes(2);
    expect(audio.src).toBe(presignedUrl('trk-1', 2).url);
    expect(audio.currentTime).toBe(0);
    expect(playback().status).toBe('playing');
  });

  it('keeps a paused track paused after re-presigning a stale seek', async () => {
    let clock = 0;
    const now = () => clock;
    const { audio, playback } = renderWebPlaybackWithClock(now);
    await act(() => playback().play(trackNamed('trk-1')));
    act(() => audio.bufferEnough());
    act(() => audio.pause());

    presign.mockResolvedValueOnce([presignedUrl('trk-1', 2)]);
    clock += PRESIGN_TTL_MS;
    await act(async () => playback().seekTo(30_000));

    expect(audio.paused).toBe(true);
    expect(playback().status).toBe('paused');
  });
});

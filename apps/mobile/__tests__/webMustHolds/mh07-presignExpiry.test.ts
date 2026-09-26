describe('mh07: a paused track resumes after its presigned URL expires', () => {
  it.skip('re-presigns and resumes from the same position when play is pressed past the presign TTL (un-skipped by "web player queue controls and presign expiry")', () => {});
});

import { createElement, type ReactNode } from 'react';
import { act, renderHook } from '@testing-library/react-native';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import { usePlayback } from '@shared/playback/usePlayback';

import { libraryTrack } from '../../src/features/playback/__tests__/fixtures';
import { WebPlaybackProvider } from '../../src/features/playback/hooks/webPlaybackProvider';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const presign = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

const PRESIGN_TTL_MS = 60 * 60 * 1000;
const TRACK_ID = asTrackId('trk-1');

class FakeAudio {
  currentSrc = '';
  private srcAttribute = '';
  currentTime = 0;
  duration = Number.NaN;
  playbackRate = 1;
  paused = true;
  ended = false;
  readyState = 4;
  error: { code: number; message: string } | null = null;
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

  emit(type: string): void {
    for (const listener of this.listeners.get(type) ?? []) listener();
  }

  play(): Promise<void> {
    this.paused = false;
    this.ended = false;
    this.emit('play');
    this.emit('playing');
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
    this.error = null;
  }
}

function presignedUrl(attempt: number): ResolvedAudioUrl {
  return {
    trackId: TRACK_ID,
    url: `https://bucket.example/trk-1.mp3?sig=${attempt}`,
    version: 'v1',
  };
}

function renderWebPlayback(now: () => number) {
  const audio = new FakeAudio();
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(
      WebPlaybackProvider,
      { createAudio: () => audio as unknown as HTMLAudioElement, now },
      children,
    );
  const rendered = renderHook(() => usePlayback(), { wrapper });
  return { audio, playback: () => rendered.result.current };
}

beforeEach(() => {
  presign.mockReset();
  presign.mockImplementation(async () => [presignedUrl(1)]);
});

describe('mh07: a paused track resumes after its presigned URL expires (un-skipped)', () => {
  it('re-presigns and resumes from the same position when play is pressed past the presign TTL', async () => {
    let clock = 0;
    const now = () => clock;
    const { audio, playback } = renderWebPlayback(now);

    await act(() =>
      playback().play(libraryTrack({ source: { kind: 'library', trackId: TRACK_ID } })),
    );
    act(() => playback().seekTo(45_000));
    act(() => playback().pause());

    presign.mockResolvedValueOnce([presignedUrl(2)]);
    clock += PRESIGN_TTL_MS;
    await act(async () => playback().resume());

    expect(presign).toHaveBeenCalledTimes(2);
    expect(audio.src).toBe(presignedUrl(2).url);
    expect(audio.currentTime).toBe(45);
    expect(playback()).toMatchObject({ status: 'playing', errorMessage: null });
  });
});

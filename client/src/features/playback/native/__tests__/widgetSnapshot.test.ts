import TrackPlayer, { Event, State } from 'react-native-track-player';

import { startWidgetSnapshotPublisher } from '@features/playback/native/widgetSnapshot';
import { asTrackId } from '@shared/apiClient/ids';
import type { PlaybackTrack } from '@shared/playback/playbackTrack';
import { useQueueStore } from '@shared/playback/queueStore';
import { notifyIdentityChange, runSignOutCleanups } from '@shared/session/signOutCleanup';
import { reloadWidget, writeCoverFromUrl, writeSnapshot } from '@modules/altune-widget';

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));
jest.mock('@modules/altune-widget', () => ({
  writeSnapshot: jest.fn(),
  writeCoverFromUrl: jest.fn(() => Promise.resolve()),
  reloadWidget: jest.fn(),
}));

const player = require('react-native-track-player').__player;

function track(id: string, artworkUrl: string | null): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: asTrackId(id) },
    title: `Track ${id}`,
    artist: 'Test Artist',
    artworkUrl,
    durationSeconds: 200,
  };
}

function settle(): Promise<void> {
  return new Promise((resolve) => setImmediate(resolve));
}

function writtenSnapshots(): unknown[] {
  return jest.mocked(writeSnapshot).mock.calls.map(([json]) => JSON.parse(json));
}

function lastSnapshot(): unknown {
  return writtenSnapshots().at(-1);
}

const RESUME_POSITION_MS = 5000;

let stop: () => void;
let onPlaybackState: (event: unknown) => void;

describe('startWidgetSnapshotPublisher', () => {
  beforeEach(async () => {
    jest.clearAllMocks();
    useQueueStore.getState().clearQueue();
    player.reset();
    jest.mocked(TrackPlayer.getPlaybackState).mockResolvedValue({ state: State.Paused });
    jest
      .mocked(TrackPlayer.getProgress)
      .mockResolvedValue({ position: 12.7, duration: 200, buffered: 0 });
    jest.mocked(TrackPlayer.addEventListener).mockReturnValue({ remove: jest.fn() } as never);
    notifyIdentityChange('user-1');
    stop = startWidgetSnapshotPublisher();
    await settle();
    onPlaybackState = player
      .calls('addEventListener')
      .find(([event]: [unknown]) => event === Event.PlaybackState)[1];
    jest.clearAllMocks();
  });

  afterEach(() => {
    stop();
  });

  it('publishes the now-playing snapshot with source read from the store, and reloads the widget', async () => {
    useQueueStore.getState().loadQueue([track('1', 'https://art/1.jpg')], 0, {
      kind: 'playlist',
      playlistId: 'p1' as never,
      name: 'Road',
    });
    await settle();

    expect(lastSnapshot()).toEqual({
      signedIn: true,
      nowPlaying: {
        title: 'Track 1',
        artist: 'Test Artist',
        source: 'Road',
        isPlaying: false,
        shuffle: false,
        repeat: 'all',
        positionSec: 12,
        durationSec: 200,
        coverFileName: 'cover.jpg',
      },
    });
    expect(writeCoverFromUrl).toHaveBeenCalledWith('https://art/1.jpg', 'cover.jpg');
    expect(reloadWidget).toHaveBeenCalledTimes(writtenSnapshots().length);
  });

  it('republishes with isPlaying when the player state changes', async () => {
    useQueueStore.getState().loadQueue([track('1', null)], 0, { kind: 'library' });
    await settle();
    jest.mocked(TrackPlayer.getPlaybackState).mockResolvedValue({ state: State.Playing });

    onPlaybackState({ state: State.Playing });
    await settle();

    expect(lastSnapshot()).toMatchObject({ nowPlaying: { isPlaying: true } });
  });

  it('writes nothing when nothing changed, and downloads a cover once per artwork url', async () => {
    useQueueStore.getState().loadQueue([track('1', 'https://art/1.jpg')], 0, { kind: 'library' });
    await settle();
    const writes = writtenSnapshots().length;

    useQueueStore.getState().setResumePosition(RESUME_POSITION_MS);
    await settle();

    expect(writtenSnapshots()).toHaveLength(writes);
    expect(writeCoverFromUrl).toHaveBeenCalledTimes(1);
  });

  it('publishes a null cover for a track without artwork and skips the download', async () => {
    useQueueStore.getState().loadQueue([track('1', null)], 0, null);
    await settle();

    expect(lastSnapshot()).toMatchObject({ nowPlaying: { source: null, coverFileName: null } });
    expect(writeCoverFromUrl).not.toHaveBeenCalled();
  });

  it('keeps publishing with a null cover when the cover write is rejected', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    jest.mocked(writeCoverFromUrl).mockRejectedValueOnce(new Error('encode failed'));

    useQueueStore.getState().loadQueue([track('1', 'https://art/1.jpg')], 0, { kind: 'library' });
    await settle();

    expect(lastSnapshot()).toMatchObject({ signedIn: true, nowPlaying: { coverFileName: null } });
  });

  it('publishes signed-in with nothing playing for an empty queue', async () => {
    useQueueStore.getState().loadQueue([track('1', null)], 0, null);
    await settle();
    useQueueStore.getState().clearQueue();
    await settle();

    expect(lastSnapshot()).toEqual({ signedIn: true, nowPlaying: null });
  });

  it('clears the snapshot to signed-out on sign-out', async () => {
    useQueueStore.getState().loadQueue([track('1', null)], 0, null);
    await settle();

    runSignOutCleanups();
    await settle();

    expect(writtenSnapshots()).toContainEqual({ signedIn: false });
    expect(reloadWidget).toHaveBeenCalledTimes(writtenSnapshots().length);
  });
});

interface Deferred {
  promise: Promise<void>;
  resolve: () => void;
}

function deferred(): Deferred {
  const pending: Deferred = { promise: Promise.resolve(), resolve: noop };
  pending.promise = new Promise<void>((done) => {
    pending.resolve = done;
  });
  return pending;
}

function noop(): void {
  return undefined;
}

function signOut(): void {
  runSignOutCleanups();
  notifyIdentityChange(null);
}

const ART_1 = 'https://art/1.jpg';
const ART_2 = 'https://art/2.jpg';
const LAST_INDEX = 2;

describe('startWidgetSnapshotPublisher, from a caller', () => {
  let stopPublisher: () => void;
  let playbackStateEvent: (event: unknown) => void;
  let subscriptionRemovals: jest.Mock[];

  beforeEach(async () => {
    jest.clearAllMocks();
    jest.mocked(writeCoverFromUrl).mockImplementation(() => Promise.resolve());
    useQueueStore.getState().clearQueue();
    useQueueStore.getState().setRepeatMode('off');
    useQueueStore.getState().setShuffled(false);
    player.reset();
    jest.mocked(TrackPlayer.getPlaybackState).mockResolvedValue({ state: State.Playing });
    jest
      .mocked(TrackPlayer.getProgress)
      .mockResolvedValue({ position: 42, duration: 200, buffered: 0 });
    jest
      .mocked(TrackPlayer.addEventListener)
      .mockImplementation(() => ({ remove: jest.fn() }) as never);
    notifyIdentityChange('user-1');
    stopPublisher = startWidgetSnapshotPublisher();
    await settle();
    playbackStateEvent = jest
      .mocked(TrackPlayer.addEventListener)
      .mock.calls.find(([event]) => event === Event.PlaybackState)?.[1] as never;
    subscriptionRemovals = jest
      .mocked(TrackPlayer.addEventListener)
      .mock.results.map((result) => (result.value as { remove: jest.Mock }).remove);
    jest.clearAllMocks();
  });

  afterEach(() => {
    stopPublisher();
    jest.mocked(writeCoverFromUrl).mockImplementation(() => Promise.resolve());
  });

  it.each([
    ['off', false],
    ['all', true],
    ['one', false],
    ['one', true],
  ] as const)(
    'publishes exactly the pinned now-playing keys for repeat %s, shuffle %s',
    async (repeat, shuffle) => {
      useQueueStore.getState().loadQueue([track('1', ART_1)], 0, {
        kind: 'playlist',
        playlistId: 'p1' as never,
        name: 'Road',
      });
      useQueueStore.getState().setRepeatMode(repeat);
      useQueueStore.getState().setShuffled(shuffle);
      await settle();

      expect(lastSnapshot()).toStrictEqual({
        signedIn: true,
        nowPlaying: {
          title: 'Track 1',
          artist: 'Test Artist',
          source: 'Road',
          isPlaying: true,
          shuffle,
          repeat,
          positionSec: 42,
          durationSec: 200,
          coverFileName: 'cover.jpg',
        },
      });
    },
  );

  it('keeps the final state of a burst of store updates', async () => {
    const store = useQueueStore.getState();
    store.loadQueue([track('1', null), track('2', null), track('3', null)], 0, null);
    store.cycleRepeatMode();
    store.setShuffled(true);
    store.skipToIndex(LAST_INDEX);
    store.setShuffled(false);
    store.setRepeatMode('one');
    await settle();

    expect(lastSnapshot()).toMatchObject({
      signedIn: true,
      nowPlaying: { title: 'Track 3', shuffle: false, repeat: 'one' },
    });
  });

  it('never leaves the first track’s slow cover on top of the second track’s', async () => {
    const slowFirst = deferred();
    const finished: string[] = [];
    jest.mocked(writeCoverFromUrl).mockImplementation((url: string) => {
      if (url === ART_1) {
        return slowFirst.promise.then(() => {
          finished.push(url);
        });
      }
      return Promise.resolve().then(() => {
        finished.push(url);
      });
    });

    useQueueStore.getState().loadQueue([track('1', ART_1), track('2', ART_2)], 0, null);
    await settle();
    useQueueStore.getState().skipToNext();
    await settle();
    slowFirst.resolve();
    await settle();
    await settle();

    expect(finished.at(-1)).toBe(ART_2);
    expect(lastSnapshot()).toMatchObject({
      nowPlaying: { title: 'Track 2', coverFileName: 'cover.jpg' },
    });
  });

  it('downloads the cover again when a track returns after another track replaced it', async () => {
    useQueueStore
      .getState()
      .loadQueue([track('1', ART_1), track('2', ART_2), track('3', ART_1)], 0, null);
    await settle();
    useQueueStore.getState().skipToNext();
    await settle();
    useQueueStore.getState().skipToNext();
    await settle();

    expect(jest.mocked(writeCoverFromUrl).mock.calls.map(([url]) => url)).toEqual([
      ART_1,
      ART_2,
      ART_1,
    ]);
  });

  it('does not download a cover again for the next track sharing the same artwork', async () => {
    useQueueStore.getState().loadQueue([track('1', ART_1), track('2', ART_1)], 0, null);
    await settle();
    useQueueStore.getState().skipToNext();
    await settle();

    expect(writeCoverFromUrl).toHaveBeenCalledTimes(1);
    expect(lastSnapshot()).toMatchObject({
      nowPlaying: { title: 'Track 2', coverFileName: 'cover.jpg' },
    });
  });

  it('drops the previous cover when the next track has no artwork', async () => {
    useQueueStore.getState().loadQueue([track('1', ART_1), track('2', null)], 0, null);
    await settle();
    useQueueStore.getState().skipToNext();
    await settle();

    expect(lastSnapshot()).toMatchObject({
      nowPlaying: { title: 'Track 2', coverFileName: null },
    });
  });

  it('publishes the next track’s cover after a rejected cover write', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    jest.mocked(writeCoverFromUrl).mockRejectedValueOnce(new Error('encode failed'));

    useQueueStore.getState().loadQueue([track('1', ART_1), track('2', ART_2)], 0, null);
    await settle();
    useQueueStore.getState().skipToNext();
    await settle();

    expect(lastSnapshot()).toMatchObject({
      nowPlaying: { title: 'Track 2', coverFileName: 'cover.jpg' },
    });
  });

  it('publishes signed-in with nothing playing for a queue loaded empty', async () => {
    useQueueStore.getState().loadQueue([track('1', null)], 0, null);
    await settle();
    useQueueStore.getState().loadQueue([], 0, null);
    await settle();

    expect(lastSnapshot()).toStrictEqual({ signedIn: true, nowPlaying: null });
  });

  it('lets no pending cover overwrite the signed-out snapshot', async () => {
    const slowCover = deferred();
    jest.mocked(writeCoverFromUrl).mockReturnValue(slowCover.promise);

    useQueueStore.getState().loadQueue([track('1', ART_1)], 0, null);
    await settle();
    signOut();
    await settle();
    slowCover.resolve();
    await settle();
    await settle();

    expect(lastSnapshot()).toStrictEqual({ signedIn: false });
  });

  it('lets no pending player read overwrite the signed-out snapshot', async () => {
    const slowProgress = deferred();
    jest
      .mocked(TrackPlayer.getProgress)
      .mockReturnValue(
        slowProgress.promise.then(() => ({ position: 42, duration: 200, buffered: 0 })),
      );

    useQueueStore.getState().loadQueue([track('1', null)], 0, null);
    signOut();
    await settle();
    slowProgress.resolve();
    await settle();
    await settle();

    expect(lastSnapshot()).toStrictEqual({ signedIn: false });
  });

  it('keeps the signed-out snapshot when the queue and player change after sign-out', async () => {
    useQueueStore.getState().loadQueue([track('1', null), track('2', null)], 0, null);
    await settle();
    signOut();
    await settle();

    useQueueStore.getState().skipToNext();
    playbackStateEvent({ state: State.Stopped });
    await settle();

    expect(lastSnapshot()).toStrictEqual({ signedIn: false });
  });

  it('publishes now playing again after signing back in', async () => {
    useQueueStore.getState().loadQueue([track('1', null)], 0, null);
    await settle();
    signOut();
    await settle();

    notifyIdentityChange('user-2');
    useQueueStore.getState().loadQueue([track('2', null)], 0, null);
    await settle();

    expect(lastSnapshot()).toMatchObject({ signedIn: true, nowPlaying: { title: 'Track 2' } });
  });

  it('writes nothing for a queue change once unsubscribed', async () => {
    useQueueStore.getState().loadQueue([track('1', null), track('2', ART_2)], 0, null);
    await settle();
    jest.mocked(writeSnapshot).mockClear();
    jest.mocked(reloadWidget).mockClear();

    stopPublisher();
    useQueueStore.getState().skipToNext();
    await settle();

    expect(writeSnapshot).not.toHaveBeenCalled();
    expect(writeCoverFromUrl).not.toHaveBeenCalled();
    expect(reloadWidget).not.toHaveBeenCalled();
  });

  it('keeps publishing the next track while a cover write never settles', async () => {
    jest.mocked(writeCoverFromUrl).mockReturnValueOnce(new Promise<void>(() => undefined));
    useQueueStore.getState().loadQueue([track('1', ART_1), track('2', null)], 0, null);
    await settle();
    useQueueStore.getState().skipToNext();
    await settle();

    expect(lastSnapshot()).toMatchObject({ nowPlaying: { title: 'Track 2', coverFileName: null } });
  });

  it('writes nothing once unsubscribed, even for a publish still waiting on its cover', async () => {
    const slowCover = deferred();
    jest.mocked(writeCoverFromUrl).mockReturnValueOnce(slowCover.promise);
    useQueueStore.getState().loadQueue([track('1', ART_1)], 0, null);
    await settle();
    jest.mocked(writeSnapshot).mockClear();
    jest.mocked(reloadWidget).mockClear();

    stopPublisher();
    slowCover.resolve();
    await settle();
    await settle();

    expect(writeSnapshot).not.toHaveBeenCalled();
    expect(reloadWidget).not.toHaveBeenCalled();
  });

  it('writes nothing on sign-out once unsubscribed', async () => {
    stopPublisher();
    signOut();
    await settle();

    expect(writeSnapshot).not.toHaveBeenCalled();
  });

  it('removes its player event subscription when unsubscribed', () => {
    stopPublisher();

    expect(subscriptionRemovals.length).toBeGreaterThan(0);
    for (const remove of subscriptionRemovals) expect(remove).toHaveBeenCalledTimes(1);
  });

  it('publishes a numeric duration for a track whose length is not known yet', async () => {
    jest
      .mocked(TrackPlayer.getProgress)
      .mockResolvedValue({ position: 0, duration: 0, buffered: 0 });
    const noLength = { ...track('1', null), durationSeconds: undefined };

    useQueueStore.getState().loadQueue([noLength], 0, null);
    await settle();

    const snapshot = lastSnapshot() as {
      nowPlaying: { durationSec: unknown; positionSec: unknown };
    };
    expect(typeof snapshot.nowPlaying.durationSec).toBe('number');
    expect(typeof snapshot.nowPlaying.positionSec).toBe('number');
  });
});

describe('startWidgetSnapshotPublisher while signed out', () => {
  let stopPublisher: () => void;

  beforeEach(async () => {
    jest.clearAllMocks();
    useQueueStore.getState().clearQueue();
    player.reset();
    jest.mocked(TrackPlayer.getPlaybackState).mockResolvedValue({ state: State.Paused });
    jest
      .mocked(TrackPlayer.getProgress)
      .mockResolvedValue({ position: 0, duration: 200, buffered: 0 });
    jest.mocked(TrackPlayer.addEventListener).mockReturnValue({ remove: jest.fn() } as never);
    notifyIdentityChange(null);
    stopPublisher = startWidgetSnapshotPublisher();
    await settle();
  });

  afterEach(() => {
    stopPublisher();
  });

  it('never publishes now playing for a queue change before anyone signs in', async () => {
    useQueueStore.getState().loadQueue([track('1', null)], 0, null);
    await settle();

    expect(writtenSnapshots()).not.toContainEqual(expect.objectContaining({ signedIn: true }));
  });
});

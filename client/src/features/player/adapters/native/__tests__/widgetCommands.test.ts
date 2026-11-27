import { nativeWidget } from '@testing/fakes/widget/nativeWidget';
import { RepeatMode, State } from 'react-native-track-player';

import { installPlaybackPort, startWidgetCommands } from '@features/player';
import { createMemoryPlaybackPort } from '@features/player/__tests__/memoryPlaybackPort';
import { asTrackId } from '@shared/catalog';
import { trackKey, type PlaybackTrack, orderedQueueTracks, useQueueStore } from '@shared/playback';
import { notifyIdentityChange } from '@shared/session';
import { authClientOver } from '@testing/fakeAuthClient';
import { recordLogs } from '@testing/fakes/log/inMemoryLogger';
import { flushPromises } from '@testing/flushPromises';

const player = require('react-native-track-player').__player;

function libraryTrack(id: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: asTrackId(id) },
    title: `Track ${id}`,
    artist: 'Test Artist',
    artworkUrl: null,
  };
}

function sendWidgetCommand(command: string): Promise<void> {
  nativeWidget.send(command);
  return flushPromises();
}

const playPrevious = jest.fn(() => Promise.resolve());

let restorePort: () => void;

describe('startWidgetCommands', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    nativeWidget.reset();
    player.reset();
    useQueueStore.getState().clearQueue();
    useQueueStore.getState().setRepeatMode('off');
    notifyIdentityChange('user-1');
    restorePort = installPlaybackPort(createMemoryPlaybackPort());
    startWidgetCommands({ playPrevious });
  });

  afterEach(() => {
    restorePort();
  });

  it('pauses a playing player on play-pause and reloads the widget once', async () => {
    player.answer('getPlaybackState', { state: State.Playing });

    await sendWidgetCommand('play-pause');

    expect(player.calls('pause')).toHaveLength(1);
    expect(player.calls('play')).toHaveLength(0);
    expect(nativeWidget.reloads()).toBe(1);
  });

  it('plays a paused player on play-pause', async () => {
    player.answer('getPlaybackState', { state: State.Paused });

    await sendWidgetCommand('play-pause');

    expect(player.calls('play')).toHaveLength(1);
    expect(player.calls('pause')).toHaveLength(0);
  });

  it('skips to the next track on next', async () => {
    await sendWidgetCommand('next');

    expect(player.calls('skipToNext')).toHaveLength(1);
    expect(nativeWidget.reloads()).toBe(1);
  });

  it('toggles shuffle on shuffle', async () => {
    useQueueStore
      .getState()
      .loadQueue([libraryTrack('a'), libraryTrack('b'), libraryTrack('c')], 0, null);

    await sendWidgetCommand('shuffle');

    expect(useQueueStore.getState().shuffled).toBe(true);
    expect(nativeWidget.reloads()).toBe(1);
  });

  it('cycles the repeat mode and applies it to the player on repeat', async () => {
    await sendWidgetCommand('repeat');

    expect(useQueueStore.getState().repeatMode).toBe('all');
    expect(player.calls('setRepeatMode')).toEqual([[RepeatMode.Queue]]);
    expect(nativeWidget.reloads()).toBe(1);
  });

  it('does nothing and does not throw for an unknown command', async () => {
    await expect(sendWidgetCommand('rewind')).resolves.toBeUndefined();
    await sendWidgetCommand('constructor');

    expect(nativeWidget.reloads()).toBe(0);
    expect(player.calls('play')).toHaveLength(0);
    expect(player.calls('pause')).toHaveLength(0);
  });

  it('runs the injected previous action on previous without priming auth when already signed in', async () => {
    await sendWidgetCommand('previous');

    expect(playPrevious).toHaveBeenCalledTimes(1);
    expect(nativeWidget.reloads()).toBe(1);
  });

  it('resolves auth before acting when a headless boot has not yet seen the session', async () => {
    const boot = bootHeadless(() => Promise.resolve(signedInSession));

    boot.send('next');
    await settle();

    expect(boot.calls('skipToNext')).toHaveLength(1);
    expect(boot.reloads()).toBe(1);
  });

  it('does nothing when signed out', async () => {
    notifyIdentityChange(null);

    await sendWidgetCommand('play-pause');
    await sendWidgetCommand('next');

    expect(player.calls('play')).toHaveLength(0);
    expect(player.calls('skipToNext')).toHaveLength(0);
    expect(nativeWidget.reloads()).toBe(0);
  });

  it('subscribes before draining held commands and unsubscribes on cleanup', () => {
    nativeWidget.reset();
    const stop = startWidgetCommands({ playPrevious });

    expect(nativeWidget.operations()).toEqual(['subscribe', 'drain']);

    stop();
    expect(nativeWidget.operations()).toEqual(['subscribe', 'drain', 'remove']);
  });
});

function settle(): Promise<void> {
  return flushPromises();
}

const SETTLE_ROUNDS = 10;
const BURST_COMMANDS = 3;
const SHUFFLE_COMMANDS = 2;

async function settleMany(): Promise<void> {
  for (let round = 0; round < SETTLE_ROUNDS; round += 1) await settle();
}

type UnhandledRejections = {
  reasons: unknown[];
  stop: () => void;
};

function collectUnhandledRejections(): UnhandledRejections {
  const reasons: unknown[] = [];
  function onUnhandled(reason: unknown): void {
    reasons.push(reason);
  }
  process.on('unhandledRejection', onUnhandled);
  return { reasons, stop: () => process.off('unhandledRejection', onUnhandled) };
}

type HeadlessBoot = {
  send: (command: string) => void;
  calls: (method: string) => unknown[];
  reloads: () => number;
  signOut: () => void;
  repeatMode: () => string;
};

const signedInSession = { data: { session: { user: { id: 'user-1' } } }, error: null };
const noSession = { data: { session: null }, error: null };

function bootHeadless(
  getSession: () => Promise<unknown>,
  bufferedAtBoot: readonly string[] = [],
): HeadlessBoot {
  let boot!: HeadlessBoot;
  jest.isolateModules(() => {
    jest.requireActual('@shared/auth').installAuthClient(authClientOver({ getSession }));
    const session = jest.requireActual('@shared/session');
    const playback = jest.requireActual('@shared/playback');
    const isolatedPlayer = jest.requireMock('react-native-track-player').__player;
    nativeWidget.hold(...bufferedAtBoot);
    jest.requireActual('@features/player').startWidgetCommands({ playPrevious });
    boot = {
      send: (command) => nativeWidget.send(command),
      calls: (method) => isolatedPlayer.calls(method),
      reloads: () => nativeWidget.reloads(),
      signOut: () => session.notifyIdentityChange(null),
      repeatMode: () => playback.useQueueStore.getState().repeatMode,
    };
  });
  return boot;
}

describe('startWidgetCommands probe', () => {
  let restore: () => void;

  beforeEach(() => {
    jest.clearAllMocks();
    nativeWidget.reset();
    player.reset();
    useQueueStore.getState().clearQueue();
    useQueueStore.getState().setRepeatMode('off');
    notifyIdentityChange('user-1');
    restore = installPlaybackPort(createMemoryPlaybackPort());
  });

  afterEach(() => {
    restore();
  });

  it('runs every command in a quick burst exactly once', async () => {
    startWidgetCommands({ playPrevious });

    nativeWidget.send('next');
    nativeWidget.send('previous');
    nativeWidget.send('next');
    await settleMany();

    expect(player.calls('skipToNext')).toEqual([[], []]);
    expect(playPrevious).toHaveBeenCalledTimes(1);
    expect(nativeWidget.reloads()).toBe(BURST_COMMANDS);
  });

  it('applies a burst of repeat taps to the player in the order they were tapped', async () => {
    startWidgetCommands({ playPrevious });

    nativeWidget.send('repeat');
    nativeWidget.send('repeat');
    await settleMany();

    expect(useQueueStore.getState().repeatMode).toBe('one');
    expect(player.calls('setRepeatMode')).toEqual([[RepeatMode.Queue], [RepeatMode.Track]]);
  });

  it('cycles repeat off to all to one and back to off, mapping each to the player mode', async () => {
    startWidgetCommands({ playPrevious });

    await sendWidgetCommand('repeat');
    expect(useQueueStore.getState().repeatMode).toBe('all');
    await sendWidgetCommand('repeat');
    expect(useQueueStore.getState().repeatMode).toBe('one');
    await sendWidgetCommand('repeat');
    expect(useQueueStore.getState().repeatMode).toBe('off');

    expect(player.calls('setRepeatMode')).toEqual([
      [RepeatMode.Queue],
      [RepeatMode.Track],
      [RepeatMode.Off],
    ]);
  });

  it('shuffle twice returns the queue to its unshuffled order', async () => {
    startWidgetCommands({ playPrevious });
    useQueueStore
      .getState()
      .loadQueue(
        [libraryTrack('a'), libraryTrack('b'), libraryTrack('c'), libraryTrack('d')],
        0,
        null,
      );

    await sendWidgetCommand('shuffle');
    await settleMany();
    await sendWidgetCommand('shuffle');
    await settleMany();

    expect(useQueueStore.getState().shuffled).toBe(false);
    expect(orderedQueueTracks(useQueueStore.getState()).map((track) => track.title)).toEqual([
      'Track a',
      'Track b',
      'Track c',
      'Track d',
    ]);
    expect(nativeWidget.reloads()).toBe(SHUFFLE_COMMANDS);
  });

  it('shuffle replaces the native upcoming tracks with the reshuffled order', async () => {
    player.answer('getActiveTrackIndex', 0);
    player.answer('getQueue', []);
    startWidgetCommands({ playPrevious });
    useQueueStore
      .getState()
      .loadQueue(
        [libraryTrack('a'), libraryTrack('b'), libraryTrack('c'), libraryTrack('d')],
        0,
        null,
      );

    await sendWidgetCommand('shuffle');
    await settleMany();

    expect(player.calls('removeUpcomingTracks')).toHaveLength(1);
    const added = (player.calls('add') as unknown[][]).flatMap(
      ([tracks]) => tracks as { title?: string }[],
    );
    expect(added.map((track) => track.title)).toEqual(
      orderedQueueTracks(useQueueStore.getState())
        .slice(1)
        .map((track) => track.title),
    );
  });

  it('shuffle from mid-list drops the played tracks from native so its index matches the store', async () => {
    const tracks = [libraryTrack('a'), libraryTrack('b'), libraryTrack('c'), libraryTrack('d')];
    const playing = 2;
    player.answer('getActiveTrackIndex', playing);
    player.answer('getActiveTrack', { id: trackKey(tracks[playing]!), url: '' });
    player.answer('getQueue', []);
    startWidgetCommands({ playPrevious });
    useQueueStore.getState().loadQueue(tracks, playing, null);

    await sendWidgetCommand('shuffle');
    await settleMany();

    expect(useQueueStore.getState().currentIndex).toBe(0);
    expect(player.calls('remove')).toEqual([[[0, 1]]]);
    const added = (player.calls('add') as unknown[][]).flatMap(
      ([added]) => added as { title?: string }[],
    );
    expect(added.map((track) => track.title).sort()).toEqual(['Track a', 'Track b', 'Track d']);
  });

  it('keeps handling later commands after the previous action rejects, without an unhandled rejection', async () => {
    const unhandled = collectUnhandledRejections();
    const failingPrevious = jest.fn(() => Promise.reject(new Error('previous failed')));
    recordLogs();
    try {
      startWidgetCommands({ playPrevious: failingPrevious });

      await sendWidgetCommand('previous');
      await settleMany();
      await sendWidgetCommand('next');
      await settleMany();

      expect(failingPrevious).toHaveBeenCalledTimes(1);
      expect(player.calls('skipToNext')).toHaveLength(1);
      expect(unhandled.reasons).toEqual([]);
    } finally {
      unhandled.stop();
    }
  });

  it('keeps handling later commands after the player rejects a skip', async () => {
    const unhandled = collectUnhandledRejections();
    recordLogs();
    try {
      startWidgetCommands({ playPrevious });
      player.failNext('skipToNext');

      await sendWidgetCommand('next');
      await settleMany();
      await sendWidgetCommand('next');
      await settleMany();

      expect(player.calls('skipToNext')).toEqual([[], []]);
      expect(unhandled.reasons).toEqual([]);
    } finally {
      unhandled.stop();
    }
  });

  it('ignores an empty command and a differently cased known command', async () => {
    startWidgetCommands({ playPrevious });

    await sendWidgetCommand('');
    await sendWidgetCommand('NEXT');
    await sendWidgetCommand(' next');

    expect(player.calls('skipToNext')).toHaveLength(0);
    expect(nativeWidget.reloads()).toBe(0);
  });

  it('ignores a command named after an object prototype member', async () => {
    startWidgetCommands({ playPrevious });

    await expect(sendWidgetCommand('toString')).resolves.toBeUndefined();
    await sendWidgetCommand('__proto__');
    await sendWidgetCommand('hasOwnProperty');

    expect(nativeWidget.reloads()).toBe(0);
  });

  it('runs a command buffered before boot once auth resolves signed in', async () => {
    const boot = bootHeadless(() => Promise.resolve(signedInSession), ['next']);
    await settleMany();

    expect(boot.calls('skipToNext')).toHaveLength(1);
    expect(boot.reloads()).toBe(1);
  });

  it('drops a command buffered before boot when auth resolves signed out', async () => {
    const boot = bootHeadless(() => Promise.resolve(noSession), ['next', 'repeat']);
    await settleMany();

    expect(boot.calls('skipToNext')).toHaveLength(0);
    expect(boot.calls('setRepeatMode')).toHaveLength(0);
    expect(boot.repeatMode()).toBe('all');
    expect(boot.reloads()).toBe(0);
  });

  it('drops a command when auth cannot be resolved because the session lookup fails', async () => {
    recordLogs();
    const boot = bootHeadless(() => Promise.reject(new Error('offline')), ['next']);
    await settleMany();

    expect(boot.calls('skipToNext')).toHaveLength(0);
    expect(boot.reloads()).toBe(0);
  });

  it('drops a waiting command when sign-out lands while auth is still resolving', async () => {
    let resolveSession!: (value: unknown) => void;
    const pendingSession = new Promise((resolve) => {
      resolveSession = resolve;
    });
    const boot = bootHeadless(() => pendingSession);

    boot.send('next');
    await settle();
    boot.signOut();
    resolveSession(signedInSession);
    await settleMany();

    expect(boot.calls('skipToNext')).toHaveLength(0);
    expect(boot.reloads()).toBe(0);
  });

  it('runs each of a burst of commands once and in order while auth is still resolving', async () => {
    let resolveSession!: (value: unknown) => void;
    const pendingSession = new Promise((resolve) => {
      resolveSession = resolve;
    });
    const boot = bootHeadless(() => pendingSession);

    boot.send('repeat');
    boot.send('repeat');
    boot.send('next');
    resolveSession(signedInSession);
    await settleMany();

    expect(boot.repeatMode()).toBe('off');
    expect(boot.calls('setRepeatMode')).toEqual([[RepeatMode.Track], [RepeatMode.Off]]);
    expect(boot.calls('skipToNext')).toHaveLength(1);
    expect(boot.reloads()).toBe(BURST_COMMANDS);
  });
});

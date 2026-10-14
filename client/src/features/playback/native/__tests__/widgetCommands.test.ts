import TrackPlayer, { RepeatMode, State } from 'react-native-track-player';

import { createMemoryPlaybackPort } from '@features/playback/__tests__/memoryPlaybackPort';
import { installPlaybackPort } from '@features/playback/ports/activePlaybackPort';
import { startWidgetCommands } from '@features/playback/native/widgetCommands';
import { asTrackId } from '@shared/apiClient/ids';
import type { PlaybackTrack } from '@shared/playback/playbackTrack';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { notifyIdentityChange } from '@shared/session/signOutCleanup';
import {
  addWidgetCommandListener,
  drainPendingCommands,
  reloadWidget,
} from '@modules/altune-widget';

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));
jest.mock('@modules/altune-widget', () => ({
  addWidgetCommandListener: jest.fn(() => ({ remove: jest.fn() })),
  drainPendingCommands: jest.fn(),
  reloadWidget: jest.fn(),
}));

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
  const listener = jest.mocked(addWidgetCommandListener).mock.calls[0]?.[0];
  listener?.({ command });
  return new Promise((resolve) => setImmediate(resolve));
}

const playPrevious = jest.fn(() => Promise.resolve());

let restorePort: () => void;

describe('startWidgetCommands', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    useQueueStore.getState().clearQueue();
    notifyIdentityChange('user-1');
    restorePort = installPlaybackPort(createMemoryPlaybackPort());
    startWidgetCommands({ playPrevious });
  });

  afterEach(() => {
    restorePort();
  });

  it('pauses a playing player on play-pause and reloads the widget once', async () => {
    jest.mocked(TrackPlayer.getPlaybackState).mockResolvedValueOnce({ state: State.Playing });

    await sendWidgetCommand('play-pause');

    expect(player.calls('pause')).toHaveLength(1);
    expect(player.calls('play')).toHaveLength(0);
    expect(reloadWidget).toHaveBeenCalledTimes(1);
  });

  it('plays a paused player on play-pause', async () => {
    jest.mocked(TrackPlayer.getPlaybackState).mockResolvedValueOnce({ state: State.Paused });

    await sendWidgetCommand('play-pause');

    expect(player.calls('play')).toHaveLength(1);
    expect(player.calls('pause')).toHaveLength(0);
  });

  it('skips to the next track on next', async () => {
    await sendWidgetCommand('next');

    expect(player.calls('skipToNext')).toHaveLength(1);
    expect(reloadWidget).toHaveBeenCalledTimes(1);
  });

  it('toggles shuffle on shuffle', async () => {
    useQueueStore
      .getState()
      .loadQueue([libraryTrack('a'), libraryTrack('b'), libraryTrack('c')], 0, null);

    await sendWidgetCommand('shuffle');

    expect(useQueueStore.getState().shuffled).toBe(true);
    expect(reloadWidget).toHaveBeenCalledTimes(1);
  });

  it('cycles the repeat mode and applies it to the player on repeat', async () => {
    await sendWidgetCommand('repeat');

    expect(useQueueStore.getState().repeatMode).toBe('all');
    expect(player.calls('setRepeatMode')).toEqual([[RepeatMode.Queue]]);
    expect(reloadWidget).toHaveBeenCalledTimes(1);
  });

  it('does nothing and does not throw for an unknown command', async () => {
    await expect(sendWidgetCommand('rewind')).resolves.toBeUndefined();
    await sendWidgetCommand('constructor');

    expect(reloadWidget).not.toHaveBeenCalled();
    expect(player.calls('play')).toHaveLength(0);
    expect(player.calls('pause')).toHaveLength(0);
  });

  it('runs the injected previous action on previous without priming auth when already signed in', async () => {
    await sendWidgetCommand('previous');

    expect(playPrevious).toHaveBeenCalledTimes(1);
    expect(reloadWidget).toHaveBeenCalledTimes(1);
  });

  it('resolves auth before acting when a headless boot has not yet seen the session', async () => {
    let skipCalls!: () => unknown[];
    let reload!: jest.Mock;
    let send!: (command: string) => void;
    jest.isolateModules(() => {
      const client = jest.requireActual('@shared/auth/supabaseClient');
      jest.spyOn(client.supabase.auth, 'getSession').mockResolvedValue({
        data: { session: { user: { id: 'user-1' } } },
        error: null,
      });
      const widget = jest.requireMock('@modules/altune-widget');
      reload = widget.reloadWidget;
      jest.requireActual('@features/playback/native/widgetCommands').startWidgetCommands({
        playPrevious,
      });
      send = (command) => widget.addWidgetCommandListener.mock.calls[0][0]({ command });
      const isolatedPlayer = jest.requireMock('react-native-track-player').__player;
      skipCalls = () => isolatedPlayer.calls('skipToNext');
    });

    send('next');
    await new Promise((resolve) => setImmediate(resolve));

    expect(skipCalls()).toHaveLength(1);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('does nothing when signed out', async () => {
    notifyIdentityChange(null);

    await sendWidgetCommand('play-pause');
    await sendWidgetCommand('next');

    expect(player.calls('play')).toHaveLength(0);
    expect(player.calls('skipToNext')).toHaveLength(0);
    expect(reloadWidget).not.toHaveBeenCalled();
  });

  it('subscribes before draining held commands and unsubscribes on cleanup', () => {
    jest.clearAllMocks();
    const stop = startWidgetCommands({ playPrevious });

    const [subscribeOrder] = jest.mocked(addWidgetCommandListener).mock.invocationCallOrder;
    const [drainOrder] = jest.mocked(drainPendingCommands).mock.invocationCallOrder;
    expect(subscribeOrder).toBeLessThan(drainOrder ?? 0);

    stop();
    const [subscription] = jest.mocked(addWidgetCommandListener).mock.results;
    expect(subscription?.value.remove).toHaveBeenCalledTimes(1);
  });
});

function settle(): Promise<void> {
  return new Promise((resolve) => setImmediate(resolve));
}

const SETTLE_ROUNDS = 10;

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
  reload: jest.Mock;
  signOut: () => void;
  repeatMode: () => string;
};

function bootHeadless(
  getSession: () => Promise<unknown>,
  bufferedAtBoot: readonly string[] = [],
): HeadlessBoot {
  let boot!: HeadlessBoot;
  jest.isolateModules(() => {
    const client = jest.requireActual('@shared/auth/supabaseClient');
    jest.spyOn(client.supabase.auth, 'getSession').mockImplementation(getSession);
    const widget = jest.requireMock('@modules/altune-widget');
    const session = jest.requireActual('@shared/session/signOutCleanup');
    const queue = jest.requireActual('@shared/playback/queueStore');
    const isolatedPlayer = jest.requireMock('react-native-track-player').__player;
    widget.drainPendingCommands.mockImplementation(() => {
      const listener = widget.addWidgetCommandListener.mock.calls[0]?.[0];
      for (const command of bufferedAtBoot) listener?.({ command });
    });
    try {
      jest.requireActual('@features/playback/native/widgetCommands').startWidgetCommands({
        playPrevious,
      });
    } finally {
      widget.drainPendingCommands.mockImplementation(() => undefined);
    }
    boot = {
      send: (command) => widget.addWidgetCommandListener.mock.calls[0][0]({ command }),
      calls: (method) => isolatedPlayer.calls(method),
      reload: widget.reloadWidget,
      signOut: () => session.notifyIdentityChange(null),
      repeatMode: () => queue.useQueueStore.getState().repeatMode,
    };
  });
  return boot;
}

const signedInSession = { data: { session: { user: { id: 'user-1' } } }, error: null };
const noSession = { data: { session: null }, error: null };

describe('startWidgetCommands probe', () => {
  let restore: () => void;

  beforeEach(() => {
    jest.clearAllMocks();
    require('react-native-track-player').__player.reset();
    useQueueStore.getState().clearQueue();
    notifyIdentityChange('user-1');
    restore = installPlaybackPort(createMemoryPlaybackPort());
  });

  afterEach(() => {
    restore();
  });

  it('runs every command in a quick burst exactly once', async () => {
    startWidgetCommands({ playPrevious });
    const listener = jest.mocked(addWidgetCommandListener).mock.calls[0]?.[0];

    listener?.({ command: 'next' });
    listener?.({ command: 'previous' });
    listener?.({ command: 'next' });
    await settleMany();

    expect(player.calls('skipToNext')).toEqual([[], []]);
    expect(playPrevious).toHaveBeenCalledTimes(1);
    expect(jest.mocked(reloadWidget).mock.calls).toEqual([[], [], []]);
  });

  it('applies a burst of repeat taps to the player in the order they were tapped', async () => {
    startWidgetCommands({ playPrevious });
    const listener = jest.mocked(addWidgetCommandListener).mock.calls[0]?.[0];

    listener?.({ command: 'repeat' });
    listener?.({ command: 'repeat' });
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
    expect(jest.mocked(reloadWidget).mock.calls).toEqual([[], []]);
  });

  it('shuffle replaces the native upcoming tracks with the reshuffled order', async () => {
    const activeIndex = jest.mocked(TrackPlayer.getActiveTrackIndex);
    const queue = jest.mocked(TrackPlayer.getQueue);
    const originalActiveIndex = activeIndex.getMockImplementation();
    const originalQueue = queue.getMockImplementation();
    activeIndex.mockResolvedValue(0);
    queue.mockResolvedValue([]);
    try {
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
    } finally {
      activeIndex.mockImplementation(originalActiveIndex);
      queue.mockImplementation(originalQueue);
    }
  });

  it('keeps handling later commands after the previous action rejects, without an unhandled rejection', async () => {
    const unhandled = collectUnhandledRejections();
    const failingPrevious = jest.fn(() => Promise.reject(new Error('previous failed')));
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
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
      warn.mockRestore();
    }
  });

  it('keeps handling later commands after the player rejects a skip', async () => {
    const unhandled = collectUnhandledRejections();
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
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
      warn.mockRestore();
    }
  });

  it('ignores an empty command and a differently cased known command', async () => {
    startWidgetCommands({ playPrevious });

    await sendWidgetCommand('');
    await sendWidgetCommand('NEXT');
    await sendWidgetCommand(' next');

    expect(player.calls('skipToNext')).toHaveLength(0);
    expect(reloadWidget).not.toHaveBeenCalled();
  });

  it('ignores a command named after an object prototype member', async () => {
    startWidgetCommands({ playPrevious });

    await expect(sendWidgetCommand('toString')).resolves.toBeUndefined();
    await sendWidgetCommand('__proto__');
    await sendWidgetCommand('hasOwnProperty');

    expect(reloadWidget).not.toHaveBeenCalled();
  });

  it('runs a command buffered before boot once auth resolves signed in', async () => {
    const boot = bootHeadless(() => Promise.resolve(signedInSession), ['next']);
    await settleMany();

    expect(boot.calls('skipToNext')).toHaveLength(1);
    expect(boot.reload).toHaveBeenCalledTimes(1);
  });

  it('drops a command buffered before boot when auth resolves signed out', async () => {
    const boot = bootHeadless(() => Promise.resolve(noSession), ['next', 'repeat']);
    await settleMany();

    expect(boot.calls('skipToNext')).toHaveLength(0);
    expect(boot.calls('setRepeatMode')).toHaveLength(0);
    expect(boot.repeatMode()).toBe('off');
    expect(boot.reload).not.toHaveBeenCalled();
  });

  it('drops a command when auth cannot be resolved because the session lookup fails', async () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    try {
      const boot = bootHeadless(() => Promise.reject(new Error('offline')), ['next']);
      await settleMany();

      expect(boot.calls('skipToNext')).toHaveLength(0);
      expect(boot.reload).not.toHaveBeenCalled();
    } finally {
      warn.mockRestore();
    }
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
    expect(boot.reload).not.toHaveBeenCalled();
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

    expect(boot.repeatMode()).toBe('one');
    expect(boot.calls('setRepeatMode')).toEqual([[RepeatMode.Queue], [RepeatMode.Track]]);
    expect(boot.calls('skipToNext')).toHaveLength(1);
    expect(boot.reload.mock.calls).toEqual([[], [], []]);
  });
});

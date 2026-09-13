import TrackPlayer, { Event, type RemoteDuckEvent } from 'react-native-track-player';

import { playbackService } from '../service';

const { __player } = jest.requireMock('react-native-track-player');

type Listener = (payload: unknown) => void;

function listenerFor(event: string): Listener {
  const calls = __player.calls('addEventListener') as [string, Listener][];
  const match = calls.filter(([name]) => name === event).at(-1);
  if (!match) throw new Error(`no listener registered for ${event}`);
  return match[1];
}

const flush = () => new Promise((resolve) => setImmediate(resolve));

async function duck(payload: RemoteDuckEvent): Promise<void> {
  listenerFor(Event.RemoteDuck)(payload);
  await flush();
}

function playWhenReadyBeforeInterruption(value: boolean): void {
  jest.mocked(TrackPlayer.getPlayWhenReady).mockResolvedValueOnce(value);
}

beforeEach(async () => {
  await playbackService();
});

describe('playbackService RemoteDuck: resume after an audio interruption such as a phone call', () => {
  it('resumes when music was playing and the interruption ends with resume allowed', async () => {
    playWhenReadyBeforeInterruption(true);

    await duck({ paused: true, permanent: false });
    expect(__player.calls('play')).toHaveLength(0);

    await duck({ paused: false, permanent: false });
    expect(__player.calls('play')).toHaveLength(1);
  });

  it('treats the omitted permanent flag the iOS payloads send as not permanent', async () => {
    playWhenReadyBeforeInterruption(true);

    await duck({ paused: true } as RemoteDuckEvent);
    await duck({ paused: false } as RemoteDuckEvent);

    expect(__player.calls('play')).toHaveLength(1);
  });

  it('stays paused when the user had paused before the interruption', async () => {
    playWhenReadyBeforeInterruption(false);

    await duck({ paused: true, permanent: false });
    await duck({ paused: false, permanent: false });

    expect(__player.calls('play')).toHaveLength(0);
  });

  it('does not resume when the interruption ends without permission to resume', async () => {
    playWhenReadyBeforeInterruption(true);

    await duck({ paused: true, permanent: false });
    await duck({ paused: true, permanent: true });
    await duck({ paused: false, permanent: false });

    expect(__player.calls('play')).toHaveLength(0);
  });

  it('does not resume when the user pauses from the remote controls mid-interruption', async () => {
    playWhenReadyBeforeInterruption(true);

    await duck({ paused: true, permanent: false });
    listenerFor(Event.RemotePause)(undefined);
    await duck({ paused: false, permanent: false });

    expect(__player.calls('pause')).toHaveLength(1);
    expect(__player.calls('play')).toHaveLength(0);
  });

  it('keeps the pre-interruption snapshot across repeated duck-start events', async () => {
    playWhenReadyBeforeInterruption(true);
    await duck({ paused: true, permanent: false });

    playWhenReadyBeforeInterruption(false);
    await duck({ paused: true, permanent: false });
    await duck({ paused: false, permanent: false });

    expect(__player.calls('play')).toHaveLength(1);
  });

  it('does not resume on an end event with no interruption open', async () => {
    await duck({ paused: false, permanent: false });

    expect(__player.calls('play')).toHaveLength(0);
  });

  it('does not resume when reading playWhenReady fails', async () => {
    __player.failNext('getPlayWhenReady');

    await duck({ paused: true, permanent: false });
    await duck({ paused: false, permanent: false });

    expect(__player.calls('play')).toHaveLength(0);
  });
});

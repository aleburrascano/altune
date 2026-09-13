import { Event, type RemoteDuckEvent } from 'react-native-track-player';

import { playbackService } from '../service';

const { __player } = jest.requireMock('react-native-track-player');

async function remoteDuckHandler(): Promise<(data: RemoteDuckEvent) => void> {
  await playbackService();
  const registration = __player
    .calls('addEventListener')
    .find(([event]: [unknown]) => event === Event.RemoteDuck);
  if (!registration) throw new Error('no RemoteDuck listener was registered');
  return registration[1];
}

describe('playbackService — RemoteDuck resumes playback after an interruption', () => {
  it('registers a RemoteDuck listener', async () => {
    const handler = await remoteDuckHandler();

    expect(typeof handler).toBe('function');
  });

  it('resumes playback when a non-permanent interruption ends', async () => {
    const handler = await remoteDuckHandler();

    handler({ paused: false, permanent: false });

    expect(__player.calls('play')).toHaveLength(1);
  });

  it('pauses when an interruption begins', async () => {
    const handler = await remoteDuckHandler();

    handler({ paused: true, permanent: false });

    expect(__player.calls('pause')).toHaveLength(1);
    expect(__player.calls('play')).toHaveLength(0);
  });

  it('stays paused on a permanent focus loss', async () => {
    const handler = await remoteDuckHandler();

    handler({ paused: true, permanent: true });

    expect(__player.calls('play')).toHaveLength(0);
    expect(__player.calls('pause')).toHaveLength(0);
  });

  it('does not fight autoHandleInterruptions: it only calls play on resume, never pause', async () => {
    const handler = await remoteDuckHandler();

    handler({ paused: false, permanent: false });

    expect(__player.calls('play')).toHaveLength(1);
    expect(__player.calls('pause')).toHaveLength(0);
  });
});

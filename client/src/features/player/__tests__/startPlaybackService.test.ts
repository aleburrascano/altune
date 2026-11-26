import {
  createStartPlaybackService,
  installPlaybackPort,
  playbackPort,
  type PlaybackPort,
} from '@features/player';
import { createMemoryPlaybackPort } from '@features/player/__tests__/memoryPlaybackPort';

const STARTS = 2;

function memoryPort(): PlaybackPort {
  return createMemoryPlaybackPort({});
}

function start(playsThroughTrackPlayer: boolean) {
  const register = jest.fn();
  const port = memoryPort();
  createStartPlaybackService({
    port,
    playsThroughTrackPlayer,
    registerPlaybackService: register,
  })();
  return { register, port };
}

let restorePort: () => void;

beforeEach(() => {
  restorePort = installPlaybackPort(memoryPort());
});

afterEach(() => {
  restorePort();
});

describe('startPlaybackService', () => {
  it('registers the service once when playback goes through the track player', () => {
    expect(start(true).register).toHaveBeenCalledTimes(1);
  });

  it('never registers the service when playback does not go through the track player', () => {
    expect(start(false).register).not.toHaveBeenCalled();
  });

  it.each([true, false])('installs its port for callers when the flag is %s', (flag) => {
    const { port } = start(flag);

    expect(playbackPort()).toBe(port);
  });

  it('registers once per start, so a later start registers again', () => {
    const register = jest.fn();
    const startIt = createStartPlaybackService({
      port: memoryPort(),
      playsThroughTrackPlayer: true,
      registerPlaybackService: register,
    });

    startIt();
    startIt();

    expect(register).toHaveBeenCalledTimes(STARTS);
  });
});

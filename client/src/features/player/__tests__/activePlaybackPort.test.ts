import {
  httpPlaybackPort,
  startPlaybackService,
  installPlaybackPort,
  playbackPort,
} from '@features/player';
import { createMemoryPlaybackPort } from '@features/player/__tests__/memoryPlaybackPort';
import type * as PlayerDoor from '@features/player';

describe('activePlaybackPort', () => {
  it('has no port until the composition root installs one', () => {
    jest.isolateModules(() => {
      const fresh: typeof PlayerDoor = require('@features/player');

      expect(() => fresh.playbackPort()).toThrow('no playback port installed');
    });
  });

  it('is the http adapter once the playback service starts', () => {
    startPlaybackService();

    expect(playbackPort()).toBe(httpPlaybackPort);
  });

  it('is the installed port until it is restored', () => {
    startPlaybackService();
    const fake = createMemoryPlaybackPort();

    const restore = installPlaybackPort(fake);
    expect(playbackPort()).toBe(fake);

    restore();
    expect(playbackPort()).toBe(httpPlaybackPort);
  });
});

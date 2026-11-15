import { httpPlaybackPort } from '@features/player/adapters/http/httpPlaybackPort';
import { startPlaybackService } from '@features/player/adapters/startPlaybackService';
import { createMemoryPlaybackPort } from '@features/player/__tests__/memoryPlaybackPort';
import { installPlaybackPort, playbackPort } from '@features/player/ports/activePlaybackPort';
import type * as ActivePlaybackPortModule from '@features/player/ports/activePlaybackPort';

describe('activePlaybackPort', () => {
  it('has no port until the composition root installs one', () => {
    jest.isolateModules(() => {
      const fresh: typeof ActivePlaybackPortModule = require('@features/player/ports/activePlaybackPort');

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

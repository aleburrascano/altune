import { httpPlaybackPort } from '@features/playback/adapters/http/httpPlaybackPort';
import { createMemoryPlaybackPort } from '@features/playback/__tests__/memoryPlaybackPort';
import { installPlaybackPort, playbackPort } from '@features/playback/ports/activePlaybackPort';

describe('activePlaybackPort', () => {
  it('is the http adapter by default', () => {
    expect(playbackPort()).toBe(httpPlaybackPort);
  });

  it('is the installed port until it is restored', () => {
    const fake = createMemoryPlaybackPort();

    const restore = installPlaybackPort(fake);
    expect(playbackPort()).toBe(fake);

    restore();
    expect(playbackPort()).toBe(httpPlaybackPort);
  });
});

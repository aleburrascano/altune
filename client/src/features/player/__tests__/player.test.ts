jest.mock('decode-uri-component', () => ({ __esModule: true, default: (s: string) => s }));

describe('@features/player public surface', () => {
  it('exports exactly the eight public names', () => {
    expect(Object.keys(require('@features/player')).sort()).toEqual(
      [
        'FullPlayer',
        'LyricsSheet',
        'MiniPlayer',
        'PlaybackProvider',
        'PlayerBar',
        'QueueSheet',
        'SleepTimerBridge',
        'startPlaybackService',
      ].sort(),
    );
  });

  it('does not expose registerPlaybackService', () => {
    expect(require('@features/player').registerPlaybackService).toBeUndefined();
  });

  it('does not load the native playback service when required', () => {
    const servicePath = require.resolve('@features/player/native/playbackService');
    require('@features/player');
    expect(Object.keys(require.cache)).not.toContain(servicePath);
  });
});

jest.mock('decode-uri-component', () => ({ __esModule: true, default: (s: string) => s }));

describe('@features/playback public surface', () => {
  it('exports exactly the eight public names', () => {
    expect(Object.keys(require('@features/playback')).sort()).toEqual(
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
    expect(require('@features/playback').registerPlaybackService).toBeUndefined();
  });

  it('does not load the native playback service when required', () => {
    const servicePath = require.resolve('@features/playback/native/playbackService');
    require('@features/playback');
    expect(Object.keys(require.cache)).not.toContain(servicePath);
  });
});

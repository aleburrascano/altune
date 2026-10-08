jest.mock('decode-uri-component', () => ({ __esModule: true, default: (s: string) => s }));

describe('@features/playback public surface', () => {
  it('exports exactly the nine public names', () => {
    expect(Object.keys(require('@features/playback')).sort()).toEqual(
      [
        'FullPlayer',
        'LyricsSheet',
        'MiniPlayer',
        'PlaybackProvider',
        'PlayerBar',
        'QueueSheet',
        'SleepTimerBridge',
        'playsThroughTrackPlayer',
        'startPlaybackService',
      ].sort(),
    );
  });

  it('does not expose registerPlaybackService', () => {
    expect(require('@features/playback').registerPlaybackService).toBeUndefined();
  });

  it('does not load the native playback service when required', () => {
    const factory = jest.fn(() => ({ registerPlaybackService: jest.fn() }));
    jest.isolateModules(() => {
      jest.doMock('@features/playback/native/registerPlaybackService', factory);
      require('@features/playback');
    });
    expect(factory).not.toHaveBeenCalled();
  });
});

const FLAG = '@features/playback/domain/playsThroughTrackPlayer';
const SERVICE = '@features/playback/native/playbackService';

function run(flag: boolean) {
  const register = jest.fn();
  const factory = jest.fn(() => ({ registerPlaybackService: register }));
  jest.isolateModules(() => {
    jest.doMock(FLAG, () => ({ playsThroughTrackPlayer: flag }));
    jest.doMock(SERVICE, factory);
    require('@features/playback/adapters/startPlaybackService').startPlaybackService();
  });
  return { register, factory };
}

describe('startPlaybackService', () => {
  it('registers the service once when the flag is true', () => {
    expect(run(true).register).toHaveBeenCalledTimes(1);
  });

  it('does nothing and never loads the service when the flag is false', () => {
    const { register, factory } = run(false);
    expect(register).not.toHaveBeenCalled();
    expect(factory).not.toHaveBeenCalled();
  });

  it('follows the flag across fresh module registries', () => {
    expect(run(true).register).toHaveBeenCalledTimes(1);
    expect(run(false).register).toHaveBeenCalledTimes(0);
  });
});

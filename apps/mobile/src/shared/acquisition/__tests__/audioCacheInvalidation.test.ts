import { asTrackId } from '@shared/api-client/ids';

import {
  registerAudioCacheInvalidator,
  invalidateAudioCaches,
  _resetAudioCacheInvalidatorsForTest,
} from '../audioCacheInvalidation';

let warnSpy: jest.SpyInstance;

beforeEach(() => {
  _resetAudioCacheInvalidatorsForTest();
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
});

afterEach(() => {
  warnSpy.mockRestore();
});

describe('_resetAudioCacheInvalidatorsForTest', () => {
  it('empties the registry so a previously registered invalidator no longer fires', () => {
    const invalidator = jest.fn();
    registerAudioCacheInvalidator(invalidator);

    _resetAudioCacheInvalidatorsForTest();
    invalidateAudioCaches(asTrackId('t0'));

    expect(invalidator).not.toHaveBeenCalled();
  });
});

describe('registerAudioCacheInvalidator / invalidateAudioCaches', () => {
  it('calls a single registered invalidator with the invalidated trackId', () => {
    const invalidator = jest.fn();
    registerAudioCacheInvalidator(invalidator);

    invalidateAudioCaches(asTrackId('t1'));

    expect(invalidator).toHaveBeenCalledWith('t1');
  });

  it('calls every registered invalidator when multiple are registered', () => {
    const first = jest.fn();
    const second = jest.fn();
    const third = jest.fn();
    registerAudioCacheInvalidator(first);
    registerAudioCacheInvalidator(second);
    registerAudioCacheInvalidator(third);

    invalidateAudioCaches(asTrackId('t2'));

    expect(first).toHaveBeenCalledWith('t2');
    expect(second).toHaveBeenCalledWith('t2');
    expect(third).toHaveBeenCalledWith('t2');
  });

  it('stops calling an invalidator once its unsubscribe function has run, while others keep firing', () => {
    const removed = jest.fn();
    const remaining = jest.fn();
    const unsubscribe = registerAudioCacheInvalidator(removed);
    registerAudioCacheInvalidator(remaining);

    unsubscribe();
    invalidateAudioCaches(asTrackId('t3'));

    expect(removed).not.toHaveBeenCalled();
    expect(remaining).toHaveBeenCalledWith('t3');
  });

  it('does not let a throwing invalidator stop a normal one registered after it', () => {
    const throwing = jest.fn(() => {
      throw new Error('disk delete failed');
    });
    const after = jest.fn();
    registerAudioCacheInvalidator(throwing);
    registerAudioCacheInvalidator(after);

    expect(() => invalidateAudioCaches(asTrackId('t4'))).not.toThrow();

    expect(throwing).toHaveBeenCalledWith('t4');
    expect(after).toHaveBeenCalledWith('t4');
  });

  it('logs the trackId and the error a throwing invalidator swallowed', () => {
    const failure = new Error('disk delete failed');
    registerAudioCacheInvalidator(() => {
      throw failure;
    });

    invalidateAudioCaches(asTrackId('t6'));

    expect(warnSpy).toHaveBeenCalledWith(expect.stringContaining('t6'), failure);
  });

  it('does not let a throwing invalidator stop a normal one registered before it', () => {
    const before = jest.fn();
    const throwing = jest.fn(() => {
      throw new Error('disk delete failed');
    });
    registerAudioCacheInvalidator(before);
    registerAudioCacheInvalidator(throwing);

    expect(() => invalidateAudioCaches(asTrackId('t5'))).not.toThrow();

    expect(before).toHaveBeenCalledWith('t5');
    expect(throwing).toHaveBeenCalledWith('t5');
  });
});

describe('track id branding', () => {
  // Compile-time guard: tsc fails if the registry starts accepting a bare string again, which is
  // what let an unparsed id reach a cache file name.
  it('refuses a bare string where a TrackId belongs', () => {
    const invalidator = jest.fn();
    registerAudioCacheInvalidator(invalidator);

    // @ts-expect-error a raw string must go through asTrackId / parseTrackId first
    invalidateAudioCaches('t7');

    expect(invalidator).toHaveBeenCalledWith('t7');
  });
});

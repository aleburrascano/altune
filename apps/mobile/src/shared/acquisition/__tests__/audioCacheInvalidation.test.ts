import {
  registerAudioCacheInvalidator,
  invalidateAudioCaches,
  _resetAudioCacheInvalidatorsForTest,
} from '../audioCacheInvalidation';

beforeEach(() => {
  _resetAudioCacheInvalidatorsForTest();
});

describe('_resetAudioCacheInvalidatorsForTest', () => {
  it('empties the registry so a previously registered invalidator no longer fires', () => {
    const invalidator = jest.fn();
    registerAudioCacheInvalidator(invalidator);

    _resetAudioCacheInvalidatorsForTest();
    invalidateAudioCaches('t0');

    expect(invalidator).not.toHaveBeenCalled();
  });
});

describe('registerAudioCacheInvalidator / invalidateAudioCaches', () => {
  it('calls a single registered invalidator with the invalidated trackId', () => {
    const invalidator = jest.fn();
    registerAudioCacheInvalidator(invalidator);

    invalidateAudioCaches('t1');

    expect(invalidator).toHaveBeenCalledWith('t1');
  });

  it('calls every registered invalidator when multiple are registered', () => {
    const first = jest.fn();
    const second = jest.fn();
    const third = jest.fn();
    registerAudioCacheInvalidator(first);
    registerAudioCacheInvalidator(second);
    registerAudioCacheInvalidator(third);

    invalidateAudioCaches('t2');

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
    invalidateAudioCaches('t3');

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

    expect(() => invalidateAudioCaches('t4')).not.toThrow();

    expect(throwing).toHaveBeenCalledWith('t4');
    expect(after).toHaveBeenCalledWith('t4');
  });

  it('does not let a throwing invalidator stop a normal one registered before it', () => {
    const before = jest.fn();
    const throwing = jest.fn(() => {
      throw new Error('disk delete failed');
    });
    registerAudioCacheInvalidator(before);
    registerAudioCacheInvalidator(throwing);

    expect(() => invalidateAudioCaches('t5')).not.toThrow();

    expect(before).toHaveBeenCalledWith('t5');
    expect(throwing).toHaveBeenCalledWith('t5');
  });
});

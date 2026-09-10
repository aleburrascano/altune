import { beginNativeLoad, endNativeLoad, shouldApplyActiveIndex } from '../nativeSyncGuard';

beforeEach(() => {
  endNativeLoad();
});

describe('shouldApplyActiveIndex — with no native load in flight', () => {
  it('applies whatever index the native player reports', () => {
    expect(shouldApplyActiveIndex(0)).toBe(true);
    expect(shouldApplyActiveIndex(3)).toBe(true);
  });
});

describe('shouldApplyActiveIndex — while a load targeting a non-zero index is in flight', () => {
  it('ignores the priming index-0 event the native add fires first', () => {
    beginNativeLoad(4);

    expect(shouldApplyActiveIndex(0)).toBe(false);
  });

  it('keeps ignoring repeated priming index-0 events until the real one arrives', () => {
    beginNativeLoad(4);

    expect(shouldApplyActiveIndex(0)).toBe(false);
    expect(shouldApplyActiveIndex(0)).toBe(false);
  });

  it('applies the real target index and then clears the guard', () => {
    beginNativeLoad(4);

    expect(shouldApplyActiveIndex(4)).toBe(true);
    expect(shouldApplyActiveIndex(0)).toBe(true);
  });
});

describe('shouldApplyActiveIndex — while a load targeting index 0 is in flight', () => {
  it('applies the index-0 event because priming to the first track is the real target', () => {
    beginNativeLoad(0);

    expect(shouldApplyActiveIndex(0)).toBe(true);
  });
});

describe('endNativeLoad — cancelling an in-flight load', () => {
  it('releases the guard so a subsequent index-0 event applies', () => {
    beginNativeLoad(4);
    endNativeLoad();

    expect(shouldApplyActiveIndex(0)).toBe(true);
  });
});

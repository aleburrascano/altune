import {
  beginNativeLoad,
  endNativeLoad,
  shouldApplyActiveIndex,
} from '../nativeSyncGuard';

describe('nativeSyncGuard', () => {
  afterEach(() => endNativeLoad());

  it('applies every index when no load is pending', () => {
    expect(shouldApplyActiveIndex(0)).toBe(true);
    expect(shouldApplyActiveIndex(3)).toBe(true);
  });

  it('drops the add()-induced index-0 transient and applies the real target', () => {
    beginNativeLoad(4);
    // add() makes index 0 active first — must be dropped so the store keeps 4.
    expect(shouldApplyActiveIndex(0)).toBe(false);
    // skip(4) reaches the target — applied, and the guard lifts.
    expect(shouldApplyActiveIndex(4)).toBe(true);
    // subsequent real transitions apply normally.
    expect(shouldApplyActiveIndex(5)).toBe(true);
  });

  it('applies immediately when the target is 0 (no skip fires)', () => {
    beginNativeLoad(0);
    expect(shouldApplyActiveIndex(0)).toBe(true);
    expect(shouldApplyActiveIndex(1)).toBe(true);
  });

  it('endNativeLoad clears a pending target (failure path)', () => {
    beginNativeLoad(2);
    endNativeLoad();
    expect(shouldApplyActiveIndex(0)).toBe(true);
  });
});

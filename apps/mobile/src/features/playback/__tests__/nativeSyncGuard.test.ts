import { beginNativeLoad, endNativeLoad, shouldApplyActiveIndex } from '../nativeSyncGuard';

describe('nativeSyncGuard', () => {
  afterEach(() => endNativeLoad());

  it('applies every index when no load is pending', () => {
    expect(shouldApplyActiveIndex(0)).toBe(true);
    expect(shouldApplyActiveIndex(3)).toBe(true);
  });

  it('drops the add()-induced index-0 transient and applies the real target', () => {
    beginNativeLoad(4);
    expect(shouldApplyActiveIndex(0)).toBe(false);
    expect(shouldApplyActiveIndex(4)).toBe(true);
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

  it('follows a real transition that races past the pending target', () => {
    beginNativeLoad(3);
    expect(shouldApplyActiveIndex(0)).toBe(false);

    expect(shouldApplyActiveIndex(4)).toBe(true);
  });

  it('does not stay pinned after a target event is missed', () => {
    beginNativeLoad(3);
    shouldApplyActiveIndex(4);

    expect(shouldApplyActiveIndex(5)).toBe(true);
    expect(shouldApplyActiveIndex(6)).toBe(true);
  });
});

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

  // A user-driven skip can land the native player on an index the pending load
  // never targets (press next while the queue is still priming). The guard must
  // still follow the native player — and must not stay pinned once a real
  // transition has been seen, or every later skip is dropped too.
  it('follows a real transition that races past the pending target', () => {
    beginNativeLoad(3);
    expect(shouldApplyActiveIndex(0)).toBe(false); // add() transient — dropped

    // User pressed next before skip(3) landed: native is now at 4, and the
    // target-3 event will never arrive.
    expect(shouldApplyActiveIndex(4)).toBe(true);
  });

  it('does not stay pinned after a target event is missed', () => {
    beginNativeLoad(3);
    shouldApplyActiveIndex(4); // target 3 skipped over — guard must release

    // Every subsequent transition must reach the store.
    expect(shouldApplyActiveIndex(5)).toBe(true);
    expect(shouldApplyActiveIndex(6)).toBe(true);
  });
});

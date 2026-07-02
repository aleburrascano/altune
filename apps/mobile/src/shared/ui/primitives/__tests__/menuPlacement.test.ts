import { resolveMenuPlacement } from '../menuPlacement';

const WINDOW_HEIGHT = 800;

describe('resolveMenuPlacement', () => {
  it('opens downward from the trigger bottom when there is room below', () => {
    const placement = resolveMenuPlacement({
      anchor: { top: 100, bottom: 140, right: 16 },
      itemCount: 4,
      windowHeight: WINDOW_HEIGHT,
      insetBottom: 34,
    });
    expect(placement).toEqual({ right: 16, top: 140 + 4 });
    expect('bottom' in placement).toBe(false);
  });

  it('flips upward from the trigger top when there is not enough room below', () => {
    const placement = resolveMenuPlacement({
      anchor: { top: 720, bottom: 760, right: 16 },
      itemCount: 4,
      windowHeight: WINDOW_HEIGHT,
      insetBottom: 34,
    });
    // menu bottom sits just above the trigger's top edge
    expect(placement).toEqual({ right: 16, bottom: WINDOW_HEIGHT - 720 + 4 });
    expect('top' in placement).toBe(false);
  });

  it('carries the trigger right offset through unchanged', () => {
    const placement = resolveMenuPlacement({
      anchor: { top: 100, bottom: 140, right: 42 },
      itemCount: 2,
      windowHeight: WINDOW_HEIGHT,
      insetBottom: 0,
    });
    expect(placement.right).toBe(42);
  });
});

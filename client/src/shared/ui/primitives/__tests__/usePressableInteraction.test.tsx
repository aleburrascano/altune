import { act, renderHook } from '@testing-library/react-native';

import { usePressableInteraction } from '@shared/ui/primitives/usePressableInteraction';

function renderInteraction() {
  return renderHook(() => usePressableInteraction(() => null));
}

describe('usePressableInteraction', () => {
  it('surfaces hovered and focused beside the props to spread on the pressable', () => {
    const { result } = renderInteraction();
    expect(result.current.hovered).toBe(false);
    expect(result.current.focused).toBe(false);

    act(() => result.current.pressableProps.onHoverIn());
    expect(result.current.hovered).toBe(true);
    act(() => result.current.pressableProps.onHoverOut());
    expect(result.current.hovered).toBe(false);

    act(() => result.current.pressableProps.onFocus());
    expect(result.current.focused).toBe(true);
    act(() => result.current.pressableProps.onBlur());
    expect(result.current.focused).toBe(false);
  });
});

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { Text, TextInput } from 'react-native';

import {
  DismissKeyboardArea,
  KeepInputFocusArea,
  adjustsKeyboardInsets,
  dismissKeyboard,
} from '@shared/ui';

type Mounted = { container: HTMLElement; root: Root };

const mounted: Mounted[] = [];

function mount(element: React.ReactElement): HTMLElement {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  mounted.push({ container, root });
  act(() => {
    root.render(element);
  });
  return container;
}

afterEach(() => {
  for (const { container, root } of mounted.splice(0)) {
    act(() => root.unmount());
    container.remove();
  }
});

function byTestId(container: HTMLElement, testID: string): HTMLElement {
  const node = container.querySelector<HTMLElement>(`[data-testid="${testID}"]`);
  if (!node) throw new Error(`${testID} did not render`);
  return node;
}

type FieldAndArea = { field: HTMLElement; area: HTMLElement };

function focusedFieldBesideDismissArea(): FieldAndArea {
  const container = mount(
    <>
      <TextInput testID="field" />
      <DismissKeyboardArea testID="area">
        <Text>child</Text>
      </DismissKeyboardArea>
    </>,
  );
  const field = byTestId(container, 'field');
  act(() => field.focus());
  return { field, area: byTestId(container, 'area') };
}

describe('keyboard (web)', () => {
  it('DismissKeyboardArea leaves the focused input focused when pressed', () => {
    const { field, area } = focusedFieldBesideDismissArea();

    act(() => area.click());

    expect(document.activeElement).toBe(field);
  });

  it('KeepInputFocusArea stops a mouse press from taking focus away from the input', () => {
    const container = mount(
      <KeepInputFocusArea testID="area">
        <Text>child</Text>
      </KeepInputFocusArea>,
    );
    const press = new MouseEvent('mousedown', { bubbles: true, cancelable: true });

    byTestId(container, 'area').dispatchEvent(press);

    expect(press.defaultPrevented).toBe(true);
  });

  it('dismissKeyboard leaves the focused input focused', () => {
    const { field } = focusedFieldBesideDismissArea();

    dismissKeyboard();

    expect(document.activeElement).toBe(field);
  });

  it('has no keyboard inset adjustment', () => {
    expect(adjustsKeyboardInsets).toBe(false);
  });
});

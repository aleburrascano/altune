import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { Text } from 'react-native';

import { HorizontalRail } from '@shared/ui/primitives/HorizontalRail.web';

function renderItem({ item }: { item: string }) {
  return <Text>{item}</Text>;
}

function itemKey(item: string): string {
  return item;
}

function renderRail(): HTMLElement {
  const container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    createRoot(container).render(
      <HorizontalRail
        testID="rail"
        data={['a', 'b', 'c']}
        keyExtractor={itemKey}
        renderItem={renderItem}
      />,
    );
  });
  const node = container.querySelector<HTMLElement>('[data-testid="rail"]');
  if (!node) {
    throw new Error('rail did not render');
  }
  Object.defineProperty(node, 'scrollWidth', { value: 500, configurable: true });
  Object.defineProperty(node, 'clientWidth', { value: 200, configurable: true });
  return node;
}

function wheel(node: HTMLElement, deltaY: number): WheelEvent {
  const event = new WheelEvent('wheel', { deltaY, deltaX: 0, bubbles: true, cancelable: true });
  node.dispatchEvent(event);
  return event;
}

describe('HorizontalRail (web)', () => {
  it('turns a vertical wheel into horizontal scroll', () => {
    const node = renderRail();
    node.scrollLeft = 0;

    const event = wheel(node, 100);

    expect(node.scrollLeft).toBe(100);
    expect(event.defaultPrevented).toBe(true);
  });

  it('lets the page scroll at the right end', () => {
    const node = renderRail();
    node.scrollLeft = 300;

    const event = wheel(node, 100);

    expect(node.scrollLeft).toBe(300);
    expect(event.defaultPrevented).toBe(false);
  });
});

import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { Text } from 'react-native';

import { HorizontalRail } from '@shared/ui/primitives/HorizontalRail.web';

const SCROLL_WIDTH = 500;
const CLIENT_WIDTH = 200;
const WHEEL_DELTA = 100;
const RIGHT_END = 300;

type RailItem = { item: string };

function renderItem({ item }: RailItem) {
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
  Object.defineProperty(node, 'scrollWidth', { value: SCROLL_WIDTH, configurable: true });
  Object.defineProperty(node, 'clientWidth', { value: CLIENT_WIDTH, configurable: true });
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

    const event = wheel(node, WHEEL_DELTA);

    expect(node.scrollLeft).toBe(WHEEL_DELTA);
    expect(event.defaultPrevented).toBe(true);
  });

  it('lets the page scroll at the right end', () => {
    const node = renderRail();
    node.scrollLeft = RIGHT_END;

    const event = wheel(node, WHEEL_DELTA);

    expect(node.scrollLeft).toBe(RIGHT_END);
    expect(event.defaultPrevented).toBe(false);
  });
});

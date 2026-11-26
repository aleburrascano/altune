import { Text } from 'react-native';
import { render, screen, within } from '@testing-library/react-native';
import type { ReactTestRendererJSON } from 'react-test-renderer';

import { setWindowWidth, WINDOW_WIDTH } from '@testing/setWindowWidth';

import { CONTENT_MAX_WIDTH, Screen } from '@shared/ui';

function singleChild(
  tree: ReactTestRendererJSON | ReactTestRendererJSON[] | null,
): ReactTestRendererJSON {
  if (!tree || Array.isArray(tree) || !tree.children || tree.children.length !== 1) {
    throw new Error('expected exactly one child in the rendered tree');
  }
  const [child] = tree.children;
  if (!child || typeof child === 'string') {
    throw new Error('expected the single child to be an element, not text');
  }
  return child;
}

describe('Screen', () => {
  it('centers a max-width column around the children on a wide screen', () => {
    setWindowWidth(WINDOW_WIDTH.desktop);

    const { toJSON } = render(
      <Screen testID="screen">
        <Text testID="content">body</Text>
      </Screen>,
    );

    const outer = screen.getByTestId('screen');
    expect(outer.props.style).toEqual(
      expect.arrayContaining([expect.objectContaining({ alignItems: 'center' })]),
    );

    const wrapper = singleChild(toJSON());
    expect(wrapper.props.style).toEqual(expect.objectContaining({ maxWidth: CONTENT_MAX_WIDTH }));
  });

  it('renders its children inside the screen on a compact screen', () => {
    setWindowWidth(WINDOW_WIDTH.phone);

    render(
      <Screen testID="screen">
        <Text testID="content">body</Text>
      </Screen>,
    );

    expect(within(screen.getByTestId('screen')).getByTestId('content')).toHaveTextContent('body');
  });
});

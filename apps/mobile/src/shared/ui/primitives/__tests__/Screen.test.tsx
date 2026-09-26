import { Text } from 'react-native';
import { render, screen } from '@testing-library/react-native';
import type { ReactTestRendererJSON } from 'react-test-renderer';

import { CONTENT_MAX_WIDTH } from '../../layout/useLayoutMode';
import { Screen } from '../Screen';

function singleChild(tree: ReactTestRendererJSON | ReactTestRendererJSON[] | null): ReactTestRendererJSON {
  if (!tree || Array.isArray(tree) || !tree.children || tree.children.length !== 1) {
    throw new Error('expected exactly one child in the rendered tree');
  }
  const [child] = tree.children;
  if (!child || typeof child === 'string') {
    throw new Error('expected the single child to be an element, not text');
  }
  return child;
}

let mockWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

jest.mock('react-native-safe-area-context', () => ({
  useSafeAreaInsets: () => ({ top: 0, bottom: 0, left: 0, right: 0 }),
}));

describe('Screen', () => {
  it('centers a max-width column around the children on a wide screen', () => {
    mockWindowWidth = 1440;

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

  it('renders children directly under the outer view on a compact screen', () => {
    mockWindowWidth = 390;

    const { toJSON } = render(
      <Screen testID="screen">
        <Text testID="content">body</Text>
      </Screen>,
    );

    const child = singleChild(toJSON());
    expect(child.type).toBe('Text');
  });
});

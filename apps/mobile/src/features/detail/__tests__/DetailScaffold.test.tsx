import { Text } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { DetailScaffold } from '../ui/DetailScaffold';

jest.mock('react-native-safe-area-context', () => ({
  useSafeAreaInsets: () => ({ top: 20, bottom: 0, left: 0, right: 0 }),
}));

function renderScaffold(overrides: Partial<Parameters<typeof DetailScaffold>[0]> = {}) {
  const onBack = jest.fn();
  const props = {
    title: 'Random Access Memories',
    artworkUrl: 'https://cdn.altune.test/rand.jpg',
    onBack,
    actions: <Text>actions-slot</Text>,
    facts: <Text>facts-slot</Text>,
    children: <Text>children-slot</Text>,
    ...overrides,
  };
  render(<DetailScaffold {...props} />);
  return { onBack };
}

describe('DetailScaffold', () => {
  it('renders the title in the banner', () => {
    renderScaffold();
    expect(screen.getByTestId('detail-banner-title')).toHaveTextContent(
      'Random Access Memories',
    );
  });

  it('renders the secondary node under the banner title', () => {
    renderScaffold({ secondary: <Text>2013</Text> });
    expect(screen.getByText('2013')).toBeTruthy();
  });

  it('calls onBack when the back button is pressed', () => {
    const { onBack } = renderScaffold();
    fireEvent.press(screen.getByTestId('detail-back'));
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it('renders the body in actions, then facts, then children order', () => {
    renderScaffold();
    const body = screen.getByTestId('detail-body');
    const texts = body.findAllByType(Text).map((node) => node.props.children);
    expect(texts).toEqual(['actions-slot', 'facts-slot', 'children-slot']);
  });

  it('has no menu button and no detail-menu testID when menuItems is absent', () => {
    renderScaffold();
    expect(screen.queryByTestId('detail-menu')).toBeNull();
  });

  it('opens the context menu on menu press and closes it on backdrop press', () => {
    const onPress = jest.fn();
    renderScaffold({ menuItems: [{ label: 'Share', onPress }] });

    expect(screen.queryByLabelText('Share')).toBeNull();

    fireEvent.press(screen.getByTestId('detail-menu'));
    expect(screen.getByLabelText('Share')).toBeTruthy();

    fireEvent.press(screen.getByLabelText('Close menu'));
    expect(screen.queryByLabelText('Share')).toBeNull();
  });

  it('runs the pressed menu item and closes the menu', () => {
    const onPress = jest.fn();
    renderScaffold({ menuItems: [{ label: 'Share', onPress }] });

    fireEvent.press(screen.getByTestId('detail-menu'));
    fireEvent.press(screen.getByLabelText('Share'));

    expect(onPress).toHaveBeenCalledTimes(1);
    expect(screen.queryByLabelText('Share')).toBeNull();
  });
});
